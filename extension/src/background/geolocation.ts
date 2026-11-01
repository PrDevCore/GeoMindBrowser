/**
 * Geolocation is fail-closed on routed origins. Chrome can answer a location
 * request from the OS provider (Wi-Fi and network beacons), which does not pass
 * through the proxy, so a routed tab could otherwise read the real location
 * while the page itself arrives from the exit.
 *
 * Blocks are added for every routed pattern and are monotonic: a policy edit
 * never lifts one, so a reload cannot open a window. Chrome drops the rules it
 * attributes to this extension when the extension is disabled or removed.
 */

import { routedTargets } from "./headers.js";

export function planGeolocationBlocks(policy: unknown): string[] {
  const seen = new Set<string>();
  const patterns: string[] = [];
  for (const target of routedTargets(policy)) {
    const pattern = target.kind === "exact" ? `*://${target.host}/*` : `*://*.${target.host}/*`;
    if (seen.has(pattern)) continue;
    seen.add(pattern);
    patterns.push(pattern);
  }
  return patterns;
}

function blockPattern(primaryPattern: string): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    chrome.contentSettings.location.set(
      { primaryPattern, setting: "block", scope: "regular" },
      () => {
        const err = chrome.runtime.lastError;
        if (err) {
          reject(new Error(err.message ?? "geolocation block could not be applied"));
          return;
        }
        resolve();
      },
    );
  });
}

/**
 * Applies every block it can, then reports the first failure so one bad pattern
 * does not leave later origins unprotected.
 */
export async function installGeolocationBlocks(policy: unknown): Promise<number> {
  const patterns = planGeolocationBlocks(policy);
  let applied = 0;
  let failure: unknown = null;
  for (const pattern of patterns) {
    try {
      await blockPattern(pattern);
      applied += 1;
    } catch (error) {
      failure ??= error;
    }
  }
  if (failure !== null) {
    throw failure;
  }
  return applied;
}
