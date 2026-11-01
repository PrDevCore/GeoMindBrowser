import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { BLOCK_PROXY, Policy, PolicyError } from "../../src/lib/policy";

const fixturesRoot = fileURLToPath(new URL("../../../shared/fixtures/policy/", import.meta.url));
const PROXY = "127.0.0.1:1080";

interface FixtureCase {
  readonly host: string;
  readonly action: "direct" | "route" | "block";
  readonly exit: string;
  readonly rule: string;
}

interface ValidFixture {
  readonly knownExits: readonly string[];
  readonly policy: unknown;
  readonly cases: readonly FixtureCase[];
}

function readJson<T>(file: string): T {
  return JSON.parse(readFileSync(file, "utf8")) as T;
}

/** Chrome passes the canonical ASCII host to the PAC script. */
function browserHost(raw: string): string {
  const host = new URL(`http://${raw}`).hostname;
  return host.endsWith(".") ? host.slice(0, -1) : host;
}

function evaluatePac(pac: string, rawHost: string): string {
  const context: { FindProxyForURL?: (url: string, host: string) => string } = {};
  vm.createContext(context);
  vm.runInContext(pac, context, { timeout: 1000 });
  const find = context.FindProxyForURL;
  if (find === undefined) throw new Error("PAC did not define FindProxyForURL");
  const host = browserHost(rawHost);
  return find(`https://${host}/`, host);
}

function expectedProxy(action: FixtureCase["action"]): string {
  if (action === "route") return `SOCKS5 ${PROXY}`;
  if (action === "block") return BLOCK_PROXY;
  return "DIRECT";
}

describe("generated PAC", () => {
  it("matches the golden file byte for byte", () => {
    const fx = readJson<ValidFixture>(path.join(fixturesRoot, "valid", "basic.json"));
    const pac = Policy.compile(fx.policy, fx.knownExits).buildPac(PROXY);
    const golden = readFileSync(path.join(fixturesRoot, "golden", "basic.pac"), "utf8");
    expect(pac).toBe(golden);
  });

  it("has exactly one DIRECT return and no rule that maps to DIRECT", () => {
    const fx = readJson<ValidFixture>(path.join(fixturesRoot, "valid", "precedence.json"));
    const pac = Policy.compile(fx.policy, fx.knownExits).buildPac(PROXY);
    expect(pac.match(/return "DIRECT";/g)).toHaveLength(1);
    expect(pac).toContain(`return "DIRECT";\n}\n`);
    expect(pac).not.toContain('"proxy":"DIRECT"');
  });

  const validFiles = readdirSync(path.join(fixturesRoot, "valid")).filter((f) => f.endsWith(".json"));
  for (const file of validFiles) {
    it(`routes every ${file} case like the matcher`, () => {
      const fx = readJson<ValidFixture>(path.join(fixturesRoot, "valid", file));
      const pac = Policy.compile(fx.policy, fx.knownExits).buildPac(PROXY);
      for (const c of fx.cases) {
        expect(evaluatePac(pac, c.host), `${c.host} (${c.rule || "default"})`).toBe(expectedProxy(c.action));
      }
    });
  }

  it("strips a trailing dot before matching", () => {
    const fx = readJson<ValidFixture>(path.join(fixturesRoot, "valid", "basic.json"));
    const pac = Policy.compile(fx.policy, fx.knownExits).buildPac(PROXY);
    expect(evaluatePac(pac, "a.example.com.")).toBe(`SOCKS5 ${PROXY}`);
  });
});

describe("PAC proxy address", () => {
  const policy = Policy.compile(
    {
      version: 1,
      defaultAction: "direct",
      rules: [{ id: "de", host: "*.example.com", action: "route", exit: "de-test" }],
    },
    ["de-test"],
  );

  it("accepts loopback with a usable port", () => {
    expect(policy.buildPac("127.0.0.1:1080")).toContain("SOCKS5 127.0.0.1:1080");
  });

  it("rejects non-loopback, reserved, and malformed addresses", () => {
    for (const addr of ["0.0.0.0:1080", "localhost:1080", "127.0.0.1:9", "127.0.0.1:70000", "127.0.0.1"]) {
      expect(() => policy.buildPac(addr), addr).toThrow(PolicyError);
    }
  });
});
