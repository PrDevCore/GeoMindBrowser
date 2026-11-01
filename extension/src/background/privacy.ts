/**
 * Global WebRTC policy. Only proxied candidates may leave the device; direct
 * UDP is disabled. This applies to every tab, including non-routed ones.
 */

export const WEBRTC_POLICY = "disable_non_proxied_udp";

export interface SettingSnapshot {
  readonly levelOfControl: string;
  readonly value: unknown;
}

export function webRtcPolicySatisfied(snapshot: SettingSnapshot): boolean {
  return snapshot.levelOfControl === "controlled_by_this_extension" && snapshot.value === WEBRTC_POLICY;
}

export function applyWebRtcPolicy(): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    chrome.privacy.network.webRTCIPHandlingPolicy.set({ value: WEBRTC_POLICY, scope: "regular" }, () => {
      const err = chrome.runtime.lastError;
      if (err) {
        reject(new Error(err.message ?? "webRTC policy could not be applied"));
        return;
      }
      resolve();
    });
  });
}

export function getWebRtcPolicy(): Promise<SettingSnapshot> {
  return new Promise<SettingSnapshot>((resolve, reject) => {
    chrome.privacy.network.webRTCIPHandlingPolicy.get({}, (details) => {
      const err = chrome.runtime.lastError;
      if (err) {
        reject(new Error(err.message ?? "webRTC policy could not be read"));
        return;
      }
      resolve({ levelOfControl: details.levelOfControl, value: details.value });
    });
  });
}

/** Re-applies the policy whenever another extension or the user overrides it. */
export function watchWebRtcPolicy(onDrift: () => void): void {
  chrome.privacy.network.webRTCIPHandlingPolicy.onChange.addListener((details) => {
    if (!webRtcPolicySatisfied(details)) {
      onDrift();
    }
  });
}
