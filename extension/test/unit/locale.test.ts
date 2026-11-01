import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { planHeaderRules } from "../../src/background/headers";
import { LOCALE_PATTERN } from "../../src/background/status";

const fixtures = JSON.parse(
  readFileSync(fileURLToPath(new URL("../../../shared/fixtures/locale/cases.json", import.meta.url)), "utf8"),
) as { valid: string[]; invalid: string[] };

const policy = {
  version: 1,
  defaultAction: "direct",
  rules: [{ id: "de", host: "example.de", action: "route", exit: "de-test" }],
};

describe("locale fixtures", () => {
  it("has both valid and invalid cases", () => {
    expect(fixtures.valid.length).toBeGreaterThan(0);
    expect(fixtures.invalid.length).toBeGreaterThan(0);
  });

  for (const locale of fixtures.valid) {
    it(`accepts ${JSON.stringify(locale)}`, () => {
      expect(LOCALE_PATTERN.test(locale)).toBe(true);
      const plan = planHeaderRules(policy, locale);
      expect(plan.entries).toHaveLength(1);
      expect(plan.entries[0]?.rule.action.requestHeaders?.[0]?.value).toBe(locale);
    });
  }

  for (const locale of fixtures.invalid) {
    it(`rejects ${JSON.stringify(locale)}`, () => {
      expect(LOCALE_PATTERN.test(locale)).toBe(false);
      expect(planHeaderRules(policy, locale).entries).toEqual([]);
    });
  }
});
