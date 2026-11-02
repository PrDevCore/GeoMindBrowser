import type { Snapshot } from "./lifecycle.js";
import { streamLink } from "./stream.js";

export type TextTarget = { setText(value: string): void };
export type ButtonTarget = { setEnabled(value: boolean): void };
export type LinkTarget = {
  setHref(value: string | null): void;
  setVisible(value: boolean): void;
};

export type Targets = {
  status: TextTarget;
  session: TextTarget;
  remaining: TextTarget;
  start: ButtonTarget;
  stop: ButtonTarget;
  open: LinkTarget;
};

const labels: Record<Snapshot["phase"], string> = {
  idle: "Ready",
  starting: "Starting",
  provisioning: "Provisioning",
  ready: "Streaming",
  ended: "Ended",
  stopped: "Stopped",
  failed: "Stopped",
};

export function render(snapshot: Snapshot, targets: Targets, now: number): void {
  targets.status.setText(`${labels[snapshot.phase]}: ${snapshot.message}`);
  targets.session.setText(snapshot.view === null ? "Session: none" : `Session: ${snapshot.view.region}`);

  const busy = snapshot.phase === "starting" || snapshot.phase === "provisioning";
  targets.start.setEnabled(!busy && snapshot.view === null);
  targets.stop.setEnabled(snapshot.view !== null);

  const link = streamLink(snapshot.view);
  if (link === null) {
    targets.open.setHref(null);
    targets.open.setVisible(false);
  } else {
    targets.open.setHref(link.href);
    targets.open.setVisible(true);
  }

  const expiresAt = snapshot.view?.expiresAt;
  if (expiresAt === undefined || link === null) {
    targets.remaining.setText("");
    return;
  }
  const seconds = Math.max(0, Math.floor((Date.parse(expiresAt) - now) / 1000));
  const minutes = Math.floor(seconds / 60);
  targets.remaining.setText(`Time remaining: ${minutes}:${String(seconds % 60).padStart(2, "0")}`);
}
