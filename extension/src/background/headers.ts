/**
 * Locale header rules: for sites routed through an exit, set Accept-Language to
 * the exit locale so the site sees the same language as the exit country. The
 * rules only touch that one request header, and only for granted hosts.
 */

import { Policy } from "../lib/policy.js";
import { LOCALE_PATTERN } from "./status.js";

const HEADER_NAME = "accept-language";
const RULE_ID_BASE = 1000;
const RULE_ID_SPAN = 1000;

export interface HeaderRule {
  id: number;
  priority: number;
  action: {
    type: "modifyHeaders";
    requestHeaders: { header: string; operation: "set"; value: string }[];
  };
  condition: {
    regexFilter: string;
  };
}

export interface HeaderPlanEntry {
  readonly rule: HeaderRule;
  readonly origin: string;
}

export interface HeaderPlan {
  readonly entries: readonly HeaderPlanEntry[];
  readonly origins: readonly string[];
}

/**
 * Builds one rule per routed pattern. Exact rules match the host and its
 * optional port only; wildcard rules require at least one label below the
 * suffix, mirroring policy matching. No locale means no rules.
 */
export function planHeaderRules(policy: unknown, locale: string | null): HeaderPlan {
  if (locale === null || !LOCALE_PATTERN.test(locale)) {
    return { entries: [], origins: [] };
  }

  const entries: HeaderPlanEntry[] = [];
  for (const [index, target] of routedTargets(policy).entries()) {
    const host = escapeHost(target.host);
    const hostPattern = target.kind === "exact" ? host : `([^/]+\\.)+${host}`;
    entries.push({
      rule: {
        id: RULE_ID_BASE + index,
        priority: 1,
        action: {
          type: "modifyHeaders",
          requestHeaders: [{ header: HEADER_NAME, operation: "set", value: locale }],
        },
        condition: { regexFilter: `^https?://${hostPattern}(:[0-9]+)?/` },
      },
      origin: target.kind === "exact" ? `*://${target.host}/*` : `*://*.${target.host}/*`,
    });
  }
  return { entries, origins: entries.map((entry) => entry.origin) };
}

/** Normalized route targets, or an empty list for an unreadable policy. */
export function routedTargets(
  policy: unknown,
): readonly { readonly host: string; readonly kind: "exact" | "wildcard" }[] {
  try {
    return Policy.compile(policy, policyExits(policy)).headerTargets();
  } catch {
    return [];
  }
}

/**
 * Replaces this extension's dynamic header rules. Rules are added only for
 * origins the user granted, so a policy edit never widens permissions. With
 * headers disabled (or a failed plan) the previous rules are removed and no
 * direct or blocked host is touched.
 */
export async function installHeaderRules(
  policy: unknown,
  locale: string | null,
  enabled: boolean,
): Promise<void> {
  const existing = await chrome.declarativeNetRequest.getDynamicRules();
  const removeRuleIds = existing.filter((rule) => isHeaderRuleId(rule.id)).map((rule) => rule.id);

  const addRules: HeaderRule[] = [];
  if (enabled) {
    for (const entry of planHeaderRules(policy, locale).entries) {
      if (await chrome.permissions.contains({ origins: [entry.origin] })) {
        addRules.push(entry.rule);
      }
    }
  }
  if (removeRuleIds.length === 0 && addRules.length === 0) {
    return;
  }
  await chrome.declarativeNetRequest.updateDynamicRules({ removeRuleIds, addRules });
}

function isHeaderRuleId(id: number): boolean {
  return id >= RULE_ID_BASE && id < RULE_ID_BASE + RULE_ID_SPAN;
}

function escapeHost(host: string): string {
  return host.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * Treat every exit named in the document as known. The companion already
 * decides which exits are usable; header planning only needs the host list and
 * must not guess routing from exit availability.
 */
function policyExits(policy: unknown): readonly string[] {
  if (typeof policy !== "object" || policy === null || Array.isArray(policy)) {
    return [];
  }
  const rules = (policy as Record<string, unknown>).rules;
  if (!Array.isArray(rules)) {
    return [];
  }
  const exits = new Set<string>();
  for (const rule of rules) {
    if (typeof rule !== "object" || rule === null || Array.isArray(rule)) continue;
    const exit = (rule as Record<string, unknown>).exit;
    if (typeof exit === "string" && exit.length > 0) exits.add(exit);
  }
  return [...exits];
}
