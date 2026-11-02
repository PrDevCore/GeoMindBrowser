import { describe, expect, it } from "vitest";
import { createControlPlane, createTokenHolder, type ApiError } from "../src/api.js";
import { emptyResponse, fakeFetch, jsonResponse, view } from "./helpers.js";

const TOKEN = "super-secret-token";

describe("control plane client", () => {
  it("sends the bearer token in a header only", async () => {
    const { fetchImpl, calls } = fakeFetch([jsonResponse(201, view())]);
    const plane = createControlPlane(fetchImpl, () => TOKEN);

    const created = await plane.create("de-test");

    expect(created.state).toBe("ready");
    expect(calls[0]?.path).toBe("/v1/sessions");
    expect(calls[0]?.path).not.toContain(TOKEN);
    const headers = calls[0]?.init.headers as Headers;
    expect(headers.get("Authorization")).toBe(`Bearer ${TOKEN}`);
    expect(headers.get("Accept")).toBe("application/json");
    expect(calls[0]?.init.credentials).toBe("omit");
    expect(calls[0]?.init.cache).toBe("no-store");
    expect(calls[0]?.init.referrerPolicy).toBe("no-referrer");
  });

  it("never repeats the token across calls or leaks it into a path", async () => {
    const holder = createTokenHolder();
    holder.set(`  ${TOKEN}  `);
    expect(holder.value()).toBe(TOKEN);

    const { fetchImpl, calls } = fakeFetch([
      jsonResponse(200, view({ id: "a b/c" })),
      emptyResponse(),
    ]);
    const plane = createControlPlane(fetchImpl, holder.value);

    await plane.get("a b/c");
    await plane.remove("a b/c");

    expect(calls[0]?.path).toBe("/v1/sessions/a%20b%2Fc");
    expect(calls[1]?.path).toBe("/v1/sessions/a%20b%2Fc");
    for (const call of calls) expect(call.path).not.toContain(TOKEN);
    holder.clear();
    expect(holder.value()).toBe("");
  });

  it("maps a rate limit onto the retry delay without retrying", async () => {
    const { fetchImpl, calls } = fakeFetch([
      jsonResponse(429, { error: "rate_limited", message: "too many requests" }, { "Retry-After": "37" }),
    ]);
    const plane = createControlPlane(fetchImpl, () => TOKEN);

    const error = await plane.create("de-test").catch((e: unknown) => e as ApiError);

    expect(error).toMatchObject({ status: 429, code: "rate_limited", retryAfterSeconds: 37 });
    expect(calls).toHaveLength(1);
  });

  it("treats 401 as unauthenticated", async () => {
    const { fetchImpl } = fakeFetch([jsonResponse(401, { error: "unauthenticated", message: "nope" })]);
    const plane = createControlPlane(fetchImpl, () => "");
    const error = await plane.get("x").catch((e: unknown) => e as ApiError);
    expect(error).toMatchObject({ status: 401, code: "unauthenticated" });
  });

  it("accepts 204 from delete and rejects a 500", async () => {
    const ok = fakeFetch([emptyResponse()]);
    const plane = createControlPlane(ok.fetchImpl, () => TOKEN);
    await expect(plane.remove("sess-1")).resolves.toBeUndefined();

    const bad = fakeFetch([jsonResponse(500, { error: "internal", message: "boom" })]);
    const failing = createControlPlane(bad.fetchImpl, () => TOKEN);
    const error = await failing.remove("sess-1").catch((e: unknown) => e as ApiError);
    expect(error).toMatchObject({ status: 500, code: "internal" });
  });

  it("checks health without attaching a token", async () => {
    const { fetchImpl, calls } = fakeFetch([jsonResponse(200, { status: "ok" })]);
    const plane = createControlPlane(fetchImpl, () => TOKEN);

    await expect(plane.health()).resolves.toBe(true);

    const headers = calls[0]?.init.headers as Headers | undefined;
    expect(headers?.get("Authorization") ?? null).toBeNull();
  });

  it("refuses to parse a malformed session view", async () => {
    const { fetchImpl } = fakeFetch([jsonResponse(201, { id: "x", state: "nonsense" })]);
    const plane = createControlPlane(fetchImpl, () => TOKEN);
    await expect(plane.create("de-test")).rejects.toThrow(/unknown state/);
  });
});
