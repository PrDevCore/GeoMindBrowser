export type State =
  | "requested"
  | "provisioning"
  | "ready"
  | "active"
  | "terminating"
  | "gone"
  | "failed";

export type SessionView = {
  id: string;
  state: State;
  region: string;
  streamUrl?: string;
  expiresAt: string;
};

const states: readonly State[] = [
  "requested",
  "provisioning",
  "ready",
  "active",
  "terminating",
  "gone",
  "failed",
];

// Mirrors session/internal/session/state.go. The client refuses an impossible
// transition instead of trusting the server, because a stale or spoofed view
// must never leave a session open that the launcher thinks is gone.
const transitions: Readonly<Record<State, readonly State[]>> = {
  requested: ["provisioning", "failed", "terminating"],
  provisioning: ["ready", "active", "failed", "terminating"],
  ready: ["active", "terminating", "failed"],
  active: ["terminating", "failed"],
  terminating: ["gone"],
  failed: ["terminating"],
  gone: [],
};

export function isState(value: unknown): value is State {
  return typeof value === "string" && states.includes(value as State);
}

export function canTransition(from: State, to: State): boolean {
  return transitions[from].includes(to);
}

export function isStreaming(state: State): boolean {
  return state === "ready" || state === "active";
}

export function isTerminal(state: State): boolean {
  return state === "gone" || state === "failed";
}

export function shouldPoll(state: State): boolean {
  return state === "requested" || state === "provisioning";
}

export function parseSessionView(value: unknown): SessionView {
  if (typeof value !== "object" || value === null) {
    throw new Error("web-client: session view must be an object");
  }
  const record = value as Record<string, unknown>;
  if (typeof record["id"] !== "string" || record["id"] === "") {
    throw new Error("web-client: session view is missing an id");
  }
  if (!isState(record["state"])) {
    throw new Error("web-client: session view has an unknown state");
  }
  if (typeof record["region"] !== "string") {
    throw new Error("web-client: session view is missing a region");
  }
  if (typeof record["expiresAt"] !== "string" || Number.isNaN(Date.parse(record["expiresAt"]))) {
    throw new Error("web-client: session view has an unusable expiresAt");
  }
  const streamUrl = record["streamUrl"];
  if (streamUrl !== undefined && typeof streamUrl !== "string") {
    throw new Error("web-client: session view has a malformed streamUrl");
  }
  if (streamUrl !== undefined && !/^https:\/\//.test(streamUrl)) {
    throw new Error("web-client: streamUrl must be an HTTPS origin");
  }
  return {
    id: record["id"],
    state: record["state"],
    region: record["region"],
    expiresAt: record["expiresAt"],
    ...(streamUrl === undefined ? {} : { streamUrl }),
  };
}
