import type { ControlPlane, ApiError } from "./api.js";
import { canTransition, isStreaming, isTerminal, shouldPoll, type SessionView } from "./session.js";

export type Phase =
  | "idle"
  | "starting"
  | "provisioning"
  | "ready"
  | "ended"
  | "stopped"
  | "failed";

export type Snapshot = {
  phase: Phase;
  view: SessionView | null;
  message: string;
};

export type Lifecycle = {
  start(region: string): Promise<void>;
  stop(): Promise<void>;
  expireCheck(): Promise<void>;
  unload(): void;
  snapshot(): Snapshot;
};

type Deps = {
  plane: ControlPlane;
  now: () => number;
  wait: (ms: number) => Promise<void>;
  pollIntervalMs?: number;
  provisioningBudgetMs?: number;
  onUpdate?: (snapshot: Snapshot) => void;
};

const defaultPollIntervalMs = 2_000;
const defaultProvisioningBudgetMs = 120_000;

function messageFor(error: unknown): string {
  const api = error as ApiError;
  if (typeof api?.code === "string") {
    switch (api.code) {
      case "rate_limited":
      case "capacity":
        return api.retryAfterSeconds === undefined
          ? "Too many sessions right now. Nothing was started."
          : `Too many sessions right now. Try again in ${api.retryAfterSeconds}s.`;
      case "unauthenticated":
        return "The token was rejected. No session is open.";
      case "not_found":
        return "That session is gone. Nothing is open.";
      case "invalid_request":
        return "The session request was refused. Nothing was started.";
      default:
        return "The session could not be reached. It was closed.";
    }
  }
  return "The connection to the control plane failed. The session was closed.";
}

export function createLifecycle(deps: Deps): Lifecycle {
  const pollIntervalMs = deps.pollIntervalMs ?? defaultPollIntervalMs;
  const budgetMs = deps.provisioningBudgetMs ?? defaultProvisioningBudgetMs;
  let current: SessionView | null = null;
  let phase: Phase = "idle";
  let message = "No session.";
  let teardownStarted = false;
  let sessionEnded = false;

  function publish(): void {
    deps.onUpdate?.({ phase, view: current, message });
  }

  async function teardown(reason: string, nextPhase: Phase): Promise<void> {
    const id = current?.id;
    if (id === undefined || teardownStarted || sessionEnded) {
      current = null;
      phase = nextPhase;
      message = reason;
      publish();
      return;
    }
    teardownStarted = true;
    sessionEnded = true;
    try {
      await deps.plane.remove(id);
      message = reason;
    } catch {
      // The control plane reaps by TTL and idle timeout, so a failed DELETE is
      // reported but never retried from the page.
      message = `${reason} The close request did not reach the control plane; the session expires on its own.`;
    }
    current = null;
    phase = nextPhase;
    publish();
  }

  function apply(next: SessionView): void {
    // The control plane may report the same state twice; only a real change has
    // to be a legal transition.
    if (current !== null && current.state !== next.state && !canTransition(current.state, next.state)) {
      throw new Error(`web-client: impossible transition ${current.state} -> ${next.state}`);
    }
    current = next;
  }

  return {
    async start(region: string): Promise<void> {
      if (phase === "starting" || phase === "provisioning" || phase === "ready") return;
      if (current !== null) return;
      teardownStarted = false;
      sessionEnded = false;
      phase = "starting";
      message = "Requesting a session…";
      publish();
      let view: SessionView;
      try {
        view = await deps.plane.create(region);
      } catch (error) {
        phase = "failed";
        message = messageFor(error);
        publish();
        return;
      }
      current = view;
      phase = "provisioning";
      message = "Provisioning the remote browser…";
      publish();

      const deadline = deps.now() + budgetMs;
      while (true) {
        if (isStreaming(view.state)) {
          phase = "ready";
          message = `Streaming from ${view.region}. Close this tab to end the session.`;
          publish();
          return;
        }
        if (isTerminal(view.state)) {
          await teardown("The session failed to start.", "failed");
          return;
        }
        if (!shouldPoll(view.state)) {
          await teardown("The session is terminating.", "stopped");
          return;
        }
        if (deps.now() > deadline) {
          await teardown("Provisioning took too long, so the session was closed.", "failed");
          return;
        }
        await deps.wait(pollIntervalMs);
        try {
          view = await deps.plane.get(view.id);
          apply(view);
        } catch (error) {
          await teardown(messageFor(error), "failed");
          return;
        }
        phase = "provisioning";
        publish();
      }
    },

    async stop(): Promise<void> {
      if (current === null) return;
      await teardown("Session closed.", "stopped");
    },

    async expireCheck(): Promise<void> {
      const view = current;
      if (view === null || !isStreaming(view.state)) return;
      if (deps.now() < Date.parse(view.expiresAt)) return;
      await teardown("The session reached its time limit.", "ended");
    },

    unload(): void {
      const id = current?.id;
      if (id === undefined || sessionEnded) return;
      sessionEnded = true;
      // keepalive lets the request survive the page going away; the TTL reaper
      // is the backstop if it still does not land.
      void deps.plane.remove(id, true).catch(() => undefined);
    },

    snapshot(): Snapshot {
      return { phase, view: current, message };
    },
  };
}
