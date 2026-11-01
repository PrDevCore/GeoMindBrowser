import { afterEach, describe, expect, it, vi } from "vitest";
import { installHeaderRules, type HeaderRule } from "../../src/background/headers";

interface Update {
  removeRuleIds?: number[];
  addRules?: HeaderRule[];
}

const policy = {
  version: 1,
  defaultAction: "direct",
  rules: [
    { id: "shop", host: "shop.example.org", action: "route", exit: "de-test" },
    { id: "wild", host: "*.example.com", action: "route", exit: "de-test" },
  ],
};

function stubChrome(existingIds: readonly number[], granted: readonly string[]): Update[] {
  const updates: Update[] = [];
  const grantedSet = new Set(granted);
  vi.stubGlobal("chrome", {
    declarativeNetRequest: {
      getDynamicRules: () => Promise.resolve(existingIds.map((id) => ({ id }))),
      updateDynamicRules: (options: Update) => {
        updates.push(options);
        return Promise.resolve();
      },
    },
    permissions: {
      contains: (query: { origins?: readonly string[] }) =>
        Promise.resolve((query.origins ?? []).every((origin) => grantedSet.has(origin))),
    },
  });
  return updates;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("installHeaderRules", () => {
  it("replaces only its own rules and adds granted hosts", async () => {
    const updates = stubChrome(
      [7, 1000],
      ["*://shop.example.org/*", "*://*.example.com/*"],
    );
    await installHeaderRules(policy, "de-DE", true);

    expect(updates).toHaveLength(1);
    expect(updates[0]?.removeRuleIds).toEqual([1000]);
    const added = updates[0]?.addRules ?? [];
    expect(added.map((rule) => rule.id)).toEqual([1000, 1001]);
    expect(added.map((rule) => rule.condition.regexFilter)).toEqual([
      "^https?://shop\\.example\\.org(:[0-9]+)?/",
      "^https?://([^/]+\\.)+example\\.com(:[0-9]+)?/",
    ]);
    expect(added[0]?.action.requestHeaders?.[0]?.value).toBe("de-DE");
  });

  it("adds only hosts the user granted", async () => {
    const updates = stubChrome([], ["*://shop.example.org/*"]);
    await installHeaderRules(policy, "de-DE", true);

    const added = updates[0]?.addRules ?? [];
    expect(added.map((rule) => rule.id)).toEqual([1000]);
  });

  it("removes its rules when headers are disabled", async () => {
    const updates = stubChrome([1000, 1001, 7], ["*://shop.example.org/*"]);
    await installHeaderRules(policy, "de-DE", false);

    expect(updates).toHaveLength(1);
    expect(updates[0]?.removeRuleIds).toEqual([1000, 1001]);
    expect(updates[0]?.addRules).toEqual([]);
  });

  it("removes stale rules when no locale is known", async () => {
    const updates = stubChrome([1000], ["*://shop.example.org/*"]);
    await installHeaderRules(policy, null, true);

    expect(updates).toHaveLength(1);
    expect(updates[0]?.removeRuleIds).toEqual([1000]);
    expect(updates[0]?.addRules).toEqual([]);
  });

  it("does not call the API when there is nothing to change", async () => {
    const updates = stubChrome([], []);
    await installHeaderRules(policy, "de-DE", false);
    expect(updates).toHaveLength(0);
  });
});
