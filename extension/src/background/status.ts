/**
 * Status parsing and badge decisions. Pure functions so they can be tested
 * without chrome.*; the service worker owns the timers and API calls.
 */

import { ProtocolError } from "./native-client.js";

export type RoutingState = "active" | "unavailable" | "error";
export type TunnelState = "up" | "down" | "starting";

export interface TunnelStatus {
  readonly state: TunnelState;
  readonly exit: string | null;
  readonly region: string | null;
  readonly locale: string | null;
  readonly since: string;
}

export interface Badge {
  readonly text: string;
  readonly title: string;
  readonly color: string;
}

const RED = "#b00020";
const AMBER = "#b26a00";
/** Same shape the companion validates before reporting a locale. */
export const LOCALE_PATTERN = /^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$/;

export function parseTunnelStatus(payload: Record<string, unknown> | undefined): TunnelStatus {
  if (payload === undefined) {
    throw new ProtocolError("malformed", "tunnel status has no payload");
  }
  const state = payload.state;
  if (state !== "up" && state !== "down" && state !== "starting") {
    throw new ProtocolError("malformed", "tunnel state is invalid");
  }
  const exit = payload.exit;
  const region = payload.region;
  const locale = payload.locale;
  const since = payload.since;
  if (exit !== null && typeof exit !== "string") {
    throw new ProtocolError("malformed", "tunnel exit is invalid");
  }
  if (region !== null && typeof region !== "string") {
    throw new ProtocolError("malformed", "tunnel region is invalid");
  }
  if (locale !== null && (typeof locale !== "string" || !LOCALE_PATTERN.test(locale))) {
    throw new ProtocolError("malformed", "tunnel locale is invalid");
  }
  if (typeof since !== "string" || since.length === 0) {
    throw new ProtocolError("malformed", "tunnel since is invalid");
  }
  return { state, exit, region, locale, since };
}

/**
 * The badge must never imply working geo-routing when the tunnel is down. A
 * down or starting tunnel keeps the PAC installed, so routed tabs still fail
 * closed; the badge reports the degraded state. A failed WebRTC policy wins
 * over everything: it can expose candidates even on unrouted tabs.
 */
export function badgeFor(
  routing: RoutingState,
  detail: string | undefined,
  tunnel: TunnelState | undefined,
  privacy?: "active" | "error",
): Badge {
  if (privacy === "error") {
    return { text: "!", title: "GeoMind: WebRTC protection failed", color: RED };
  }
  if (routing === "active") {
    if (tunnel === "down") {
      return { text: "!", title: "GeoMind: tunnel down", color: RED };
    }
    if (tunnel === "starting") {
      return { text: "…", title: "GeoMind: tunnel starting", color: AMBER };
    }
    return { text: "", title: "GeoMind: routing active", color: RED };
  }
  if (routing === "unavailable") {
    return { text: "!", title: "GeoMind: no policy loaded", color: RED };
  }
  const suffix = detail === undefined ? "" : ` (${detail})`;
  return { text: "!", title: `GeoMind: routing error${suffix}`, color: RED };
}
