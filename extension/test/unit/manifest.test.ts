import { createHash, createPublicKey } from "node:crypto";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const manifestPath = fileURLToPath(new URL("../../manifest.json", import.meta.url));
const manifest = JSON.parse(readFileSync(manifestPath, "utf8")) as Record<string, unknown>;

function asRecord(value: unknown): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error("expected an object");
  }
  return value as Record<string, unknown>;
}

describe("MV3 manifest", () => {
  it("requests only the permissions this step uses", () => {
    expect(manifest.manifest_version).toBe(3);
    expect(manifest.permissions).toEqual([
      "nativeMessaging",
      "storage",
      "proxy",
      "privacy",
      "declarativeNetRequestWithHostAccess",
    ]);
    expect(manifest.host_permissions).toBeUndefined();
    expect(manifest.optional_host_permissions).toEqual(["*://*/*"]);
    expect(manifest.content_scripts).toBeUndefined();
    expect(JSON.stringify(manifest)).not.toContain("<all_urls>");
  });

  it("ships an options page for per-site locale headers", () => {
    const options = asRecord(manifest.options_ui);
    expect(options.page).toBe("options.html");
    expect(options.open_in_tab).toBe(true);
  });

  it("runs a module service worker with a badge-only action", () => {
    const background = asRecord(manifest.background);
    expect(background.type).toBe("module");
    expect(background.service_worker).toBe("dist/background/service-worker.js");
    const action = asRecord(manifest.action);
    expect(action.default_title).toBe("GeoMind");
    expect(action.default_popup).toBeUndefined();
    expect(manifest.side_panel).toBeUndefined();
  });

  it("carries a stable 2048-bit extension key and a valid derived id", () => {
    const key = manifest.key;
    if (typeof key !== "string") {
      throw new Error("manifest key is missing");
    }
    const der = Buffer.from(key, "base64");
    const publicKey = createPublicKey({ key: der, format: "der", type: "spki" });
    expect(publicKey.asymmetricKeyType).toBe("rsa");
    expect(publicKey.asymmetricKeyDetails?.modulusLength).toBe(2048);

    const hash = createHash("sha256").update(der).digest().subarray(0, 16);
    let id = "";
    for (const byte of hash) {
      id += "abcdefghijklmnop"[byte >> 4] ?? "";
      id += "abcdefghijklmnop"[byte & 15] ?? "";
    }
    expect(id).toMatch(/^[a-p]{32}$/);
  });

  it("targets a Chrome version that supports session storage", () => {
    const version = manifest.minimum_chrome_version;
    if (typeof version !== "string") {
      throw new Error("minimum_chrome_version is missing");
    }
    expect(Number.parseInt(version, 10)).toBeGreaterThanOrEqual(116);
  });

  it("ships toolbar and store icons at every declared size", () => {
    const icons = asRecord(manifest.icons);
    const actionIcons = asRecord(asRecord(manifest.action).default_icon);
    for (const size of ["16", "32", "48", "128"]) {
      const path = icons[size];
      expect(typeof path).toBe("string");
      expect(actionIcons[size]).toBe(path);
      expect(pngSize(path as string)).toEqual({ width: Number(size), height: Number(size) });
    }
  });
});

function pngSize(relativePath: string): { width: number; height: number } {
  const png = readFileSync(fileURLToPath(new URL(`../../${relativePath}`, import.meta.url)));
  if (png.subarray(1, 4).toString("ascii") !== "PNG") {
    throw new Error(`${relativePath} is not a PNG`);
  }
  return { width: png.readUInt32BE(16), height: png.readUInt32BE(20) };
}
