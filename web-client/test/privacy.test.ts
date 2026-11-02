import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const srcDir = fileURLToPath(new URL("../src/", import.meta.url));
const rootDir = fileURLToPath(new URL("../", import.meta.url));
const sources = readdirSync(srcDir).filter((name) => name.endsWith(".ts"));

function readSource(name: string): string {
  return readFileSync(srcDir + name, "utf8");
}

// The launcher holds a bearer token and starts infrastructure that costs money.
// These are the "no persistence, no telemetry" rules from AGENTS.md, enforced on
// the source rather than by intent.
describe("privacy invariants", () => {
  it("never touches browser storage or cookies", () => {
    for (const name of sources) {
      const text = readSource(name);
      for (const forbidden of ["localStorage", "sessionStorage", "indexedDB", "document.cookie", "caches"]) {
        expect(text, `${name} must not use ${forbidden}`).not.toContain(forbidden);
      }
    }
  });

  it("never writes to the console", () => {
    for (const name of sources) {
      expect(readSource(name)).not.toMatch(/console\.(log|info|warn|error|debug)/);
    }
  });

  it("has no fetch of a third-party origin", () => {
    for (const name of sources) {
      expect(readSource(name)).not.toMatch(/https?:\/\/[a-z0-9.-]+\.[a-z]{2,}/i);
    }
  });

  it("serves the page with a deny-by-default CSP and no referrer", () => {
    const html = readFileSync(rootDir + "index.html", "utf8");
    const csp = /content="([^"]*)"/.exec(html)?.[1] ?? "";
    expect(csp).toContain("default-src 'none'");
    expect(csp).toContain("connect-src 'self'");
    expect(csp).toContain("script-src 'self'");
    expect(csp).not.toContain("unsafe-inline");
    expect(csp).not.toContain("unsafe-eval");
    expect(html).toContain('name="referrer" content="no-referrer"');
    expect(html).not.toMatch(/<script(?![^>]*type="module"[^>]*src=)[^>]*>/i);
  });

  it("opens the stream in a fresh tab that leaks nothing back", () => {
    const html = readFileSync(rootDir + "index.html", "utf8");
    expect(html).toMatch(/id="open"[^>]*rel="noreferrer noopener"/);
    expect(html).toMatch(/id="open"[^>]*target="_blank"/);
    expect(html).toMatch(/id="token"[^>]*type="password"/);
    expect(html).toMatch(/id="token"[^>]*autocomplete="off"/);
  });

  it("disables the destructive controls in markup so a slow script cannot double-start", () => {
    const html = readFileSync(rootDir + "index.html", "utf8");
    expect(html).toMatch(/id="stop"[^>]*disabled/);
    expect(html).toMatch(/id="open"[^>]*hidden/);
  });
});
