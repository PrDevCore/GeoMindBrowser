/**
 * Minimal rule page: shows what the companion routes, and grants or removes the
 * per-site host access needed to set Accept-Language for those sites.
 */

import { installHeaderRules, planHeaderRules, routedTargets, type HeaderPlan } from "../background/headers.js";
import { HEADER_ORIGINS_KEY, HEADERS_ENABLED_KEY, POLICY_KEY, PRIVACY_KEY, STATUS_KEY } from "../lib/keys.js";

interface StatusView {
  readonly state?: string;
  readonly routing?: string;
  readonly ruleCount?: number;
  readonly code?: string;
  readonly tunnel?: { readonly state?: string; readonly locale?: string | null };
}

interface PrivacyView {
  readonly state?: string;
}

const statusText = element<HTMLParagraphElement>("status");
const privacyText = element<HTMLParagraphElement>("privacy");
const localeText = element<HTMLParagraphElement>("locale");
const hostsList = element<HTMLUListElement>("hosts");
const hostsEmpty = element<HTMLParagraphElement>("hosts-empty");
const headersState = element<HTMLParagraphElement>("headers-state");
const enableButton = element<HTMLButtonElement>("enable");
const disableButton = element<HTMLButtonElement>("disable");

let currentPolicy: unknown = null;
let currentLocale: string | null = null;
let currentPlan: HeaderPlan = { entries: [], origins: [] };

function element<T extends HTMLElement>(id: string): T {
  const found = document.getElementById(id);
  if (found === null) {
    throw new Error(`missing element #${id}`);
  }
  return found as T;
}

async function readSession<T>(key: string): Promise<T | null> {
  const all = await chrome.storage.session.get(key);
  return (all[key] as T | undefined) ?? null;
}

async function headersEnabled(): Promise<boolean> {
  const all = await chrome.storage.local.get(HEADERS_ENABLED_KEY);
  return all[HEADERS_ENABLED_KEY] === true;
}

async function grantedOrigins(): Promise<string[]> {
  const all = await chrome.storage.local.get(HEADER_ORIGINS_KEY);
  const raw = all[HEADER_ORIGINS_KEY];
  return Array.isArray(raw) ? raw.filter((value): value is string => typeof value === "string") : [];
}

function describeStatus(status: StatusView | null): string {
  if (status === null) {
    return "Companion: no status yet.";
  }
  if (status.state === "disconnected") {
    return "Companion disconnected. Routed sites stay blocked until it reconnects.";
  }
  if (status.state === "error") {
    return `Companion error: ${status.code ?? "unknown"}.`;
  }
  if (status.routing === "active") {
    const rules = status.ruleCount === undefined ? "" : ` (${status.ruleCount} rules)`;
    const tunnel = status.tunnel?.state ?? "unknown";
    return `Routing active${rules}. Tunnel: ${tunnel}.`;
  }
  if (status.routing === "unavailable") {
    return "Routing unavailable: no policy is loaded.";
  }
  return "Routing error: the companion policy could not be applied.";
}

async function refresh(): Promise<void> {
  const [status, policy, enabled, privacy, storedOrigins] = await Promise.all([
    readSession<StatusView>(STATUS_KEY),
    readSession<unknown>(POLICY_KEY),
    headersEnabled(),
    readSession<PrivacyView>(PRIVACY_KEY),
    grantedOrigins(),
  ]);

  const locale = status?.tunnel?.locale ?? null;
  currentPolicy = policy;
  currentLocale = locale;
  currentPlan = planHeaderRules(policy, locale);

  statusText.textContent = describeStatus(status);
  privacyText.textContent =
    privacy === null
      ? "WebRTC protection: unknown."
      : privacy.state === "active"
        ? "WebRTC protection: active (non-proxied UDP disabled)."
        : "WebRTC protection failed: direct UDP may expose your address.";
  localeText.textContent = locale === null ? "Exit locale: not reported" : `Exit locale: ${locale}`;

  const targets = routedTargets(policy);
  hostsList.replaceChildren(...targets.map((target) => hostItem(target)));
  hostsEmpty.hidden = targets.length > 0;

  // The grant exists only for header rules; drop sites that are no longer
  // routed. Skip when the policy is not loaded yet so a transient empty state
  // cannot revoke grants the user made.
  if (targets.length > 0) {
    const stale = storedOrigins.filter((origin) => !currentPlan.origins.includes(origin));
    if (stale.length > 0) {
      await chrome.permissions.remove({ origins: stale });
      await chrome.storage.local.set({
        [HEADER_ORIGINS_KEY]: storedOrigins.filter((origin) => !stale.includes(origin)),
      });
    }
  }

  const permitted =
    currentPlan.origins.length > 0 &&
    (await chrome.permissions.contains({ origins: [...currentPlan.origins] }));

  if (!enabled) {
    headersState.textContent = "Locale headers are off.";
  } else if (!permitted) {
    headersState.textContent =
      "Locale headers are enabled but the site permission is missing. Enable again to grant it.";
  } else {
    headersState.textContent = `Locale headers are on for ${currentPlan.entries.length} routed site(s).`;
  }

  enableButton.disabled = currentPlan.entries.length === 0 || (enabled && permitted);
  disableButton.disabled = !enabled && !permitted;
}

function hostItem(target: { readonly host: string; readonly kind: "exact" | "wildcard" }): HTMLLIElement {
  const item = document.createElement("li");
  item.textContent = target.kind === "wildcard" ? `*.${target.host}` : target.host;
  return item;
}

function showPermissionDenied(): void {
  headersState.textContent = "Permission was not granted. Locale headers stay off.";
}

enableButton.addEventListener("click", () => {
  const origins = [...currentPlan.origins];
  if (origins.length === 0) {
    return;
  }
  // Requested synchronously in the click handler so the user gesture is valid.
  void chrome.permissions
    .request({ origins })
    .then(async (granted) => {
      if (!granted) {
        showPermissionDenied();
        return;
      }
      await chrome.storage.local.set({
        [HEADERS_ENABLED_KEY]: true,
        [HEADER_ORIGINS_KEY]: origins,
      });
      await installHeaderRules(currentPolicy, currentLocale, true);
      await refresh();
    })
    .catch(showPermissionDenied);
});

disableButton.addEventListener("click", () => {
  void grantedOrigins()
    .then(async (storedOrigins) => {
      const origins = [...new Set([...currentPlan.origins, ...storedOrigins])];
      if (origins.length > 0) {
        await chrome.permissions.remove({ origins });
      }
      await chrome.storage.local.set({
        [HEADERS_ENABLED_KEY]: false,
        [HEADER_ORIGINS_KEY]: [],
      });
      await installHeaderRules(currentPolicy, currentLocale, false);
      await refresh();
    })
    .catch(() => {
      headersState.textContent = "Could not remove the site permission.";
    });
});

chrome.storage.onChanged.addListener((_changes, area) => {
  if (area === "session" || area === "local") {
    void refresh();
  }
});

void refresh();
