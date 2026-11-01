import { describe, expect, it } from "vitest";
import { planHeaderRules, routedTargets } from "../../src/background/headers";

const policy = {
  version: 1,
  defaultAction: "direct",
  rules: [
    { id: "shop", host: "shop.example.org", action: "route", exit: "de-test" },
    { id: "wild", host: "*.example.com", action: "route", exit: "de-test" },
    { id: "blocked", host: "*.blocked.test", action: "block" },
  ],
};

function matches(source: string, url: string): boolean {
  return new RegExp(source).test(url);
}

describe("planHeaderRules", () => {
  it("plans one Accept-Language rule per routed pattern", () => {
    const plan = planHeaderRules(policy, "de-DE");
    expect(plan.entries).toHaveLength(2);
    expect(plan.origins).toEqual(["*://shop.example.org/*", "*://*.example.com/*"]);

    const [exact, wildcard] = plan.entries;
    if (exact === undefined || wildcard === undefined) {
      throw new Error("missing entries");
    }
    expect(exact.rule.id).toBe(1000);
    expect(wildcard.rule.id).toBe(1001);
    for (const entry of plan.entries) {
      expect(entry.rule.action).toEqual({
        type: "modifyHeaders",
        requestHeaders: [{ header: "accept-language", operation: "set", value: "de-DE" }],
      });
    }
  });

  it("anchors exact rules to the host and optional port", () => {
    const exact = planHeaderRules(policy, "de-DE").entries[0]?.rule;
    if (exact === undefined) throw new Error("missing exact rule");
    expect(exact.condition.regexFilter).toBe("^https?://shop\\.example\\.org(:[0-9]+)?/");
    expect(matches(exact.condition.regexFilter, "https://shop.example.org/")).toBe(true);
    expect(matches(exact.condition.regexFilter, "http://shop.example.org:8443/path?q=1")).toBe(true);
    expect(matches(exact.condition.regexFilter, "https://sub.shop.example.org/")).toBe(false);
    expect(matches(exact.condition.regexFilter, "https://evilshop.example.org/")).toBe(false);
  });

  it("requires at least one label below a wildcard suffix", () => {
    const wildcard = planHeaderRules(policy, "de-DE").entries[1]?.rule;
    if (wildcard === undefined) throw new Error("missing wildcard rule");
    expect(wildcard.condition.regexFilter).toBe("^https?://([^/]+\\.)+example\\.com(:[0-9]+)?/");
    expect(matches(wildcard.condition.regexFilter, "https://a.example.com/")).toBe(true);
    expect(matches(wildcard.condition.regexFilter, "https://a.b.example.com:8080/")).toBe(true);
    expect(matches(wildcard.condition.regexFilter, "https://example.com/")).toBe(false);
    expect(matches(wildcard.condition.regexFilter, "https://notexample.com/")).toBe(false);
    expect(matches(wildcard.condition.regexFilter, "https://evil.test/?next=https://a.example.com/")).toBe(
      false,
    );
  });

  it("normalizes international hosts to punycode", () => {
    const plan = planHeaderRules(
      {
        version: 1,
        defaultAction: "direct",
        rules: [{ id: "de", host: "bücher.example", action: "route", exit: "de-test" }],
      },
      "de-DE",
    );
    expect(plan.origins).toEqual(["*://xn--bcher-kva.example/*"]);
    expect(plan.entries[0]?.rule.condition.regexFilter).toContain("xn--bcher-kva\\.example");
  });

  it("deduplicates repeated route targets and ignores block rules", () => {
    const plan = planHeaderRules(
      {
        version: 1,
        defaultAction: "direct",
        rules: [
          { id: "a", host: "example.com", action: "route", exit: "de-test" },
          { id: "b", host: "example.com", action: "route", exit: "de-test" },
          { id: "c", host: "blocked.test", action: "block" },
        ],
      },
      "de-DE",
    );
    expect(plan.entries).toHaveLength(1);
    expect(routedTargets(policy).map((target) => target.host)).toEqual([
      "shop.example.org",
      "example.com",
    ]);
  });

  it("plans nothing without a valid locale", () => {
    expect(planHeaderRules(policy, null).entries).toEqual([]);
    expect(planHeaderRules(policy, "").entries).toEqual([]);
    expect(planHeaderRules(policy, "de_DE").entries).toEqual([]);
    expect(planHeaderRules(policy, "de-DE\r\nX-Evil: 1").entries).toEqual([]);
  });

  it("plans nothing for a malformed policy", () => {
    expect(planHeaderRules(null, "de-DE").entries).toEqual([]);
    expect(planHeaderRules({ version: 2 }, "de-DE").entries).toEqual([]);
    expect(
      planHeaderRules(
        { version: 1, defaultAction: "direct", rules: [{ id: "x", host: "a/../b", action: "route", exit: "de" }] },
        "de-DE",
      ).entries,
    ).toEqual([]);
  });
});
