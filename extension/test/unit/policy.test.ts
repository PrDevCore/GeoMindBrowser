import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { Policy, PolicyError } from "../../src/lib/policy";

const fixturesRoot = fileURLToPath(new URL("../../../shared/fixtures/policy/", import.meta.url));

interface FixtureCase {
  readonly host: string;
  readonly action: string;
  readonly exit: string;
  readonly rule: string;
}

interface ValidFixture {
  readonly name: string;
  readonly knownExits: readonly string[];
  readonly policy: unknown;
  readonly cases: readonly FixtureCase[];
}

interface InvalidFixture {
  readonly knownExits: readonly string[];
  readonly cases: readonly { readonly why: string; readonly policy: unknown }[];
}

function readJson<T>(file: string): T {
  return JSON.parse(readFileSync(file, "utf8")) as T;
}

const validFiles = readdirSync(path.join(fixturesRoot, "valid")).filter((f) => f.endsWith(".json"));

describe("valid policy fixtures", () => {
  for (const file of validFiles) {
    it(file, () => {
      const fx = readJson<ValidFixture>(path.join(fixturesRoot, "valid", file));
      const policy = Policy.compile(fx.policy, fx.knownExits);
      for (const c of fx.cases) {
        expect(policy.match(c.host), c.host).toEqual({
          action: c.action,
          exit: c.exit,
          ruleId: c.rule,
        });
      }
    });
  }
});

const invalid = readJson<InvalidFixture>(path.join(fixturesRoot, "invalid", "cases.json"));

describe("invalid policy fixtures", () => {
  for (const c of invalid.cases) {
    it(c.why, () => {
      expect(() => Policy.compile(c.policy, invalid.knownExits)).toThrow(PolicyError);
    });
  }
});

describe("normalization", () => {
  it("rejects IP literals, ports, and wildcard misuse", () => {
    const bad = [
      "127.0.0.1",
      "example.com:443",
      "http://example.com",
      "a.*.com",
      "a..b",
      "a_b.com",
      " example.com",
    ];
    for (const host of bad) {
      expect(() => Policy.compile({ version: 1, defaultAction: "direct", rules: [{ id: "x", host, action: "block" }] }, []), host).toThrow(PolicyError);
    }
  });
});
