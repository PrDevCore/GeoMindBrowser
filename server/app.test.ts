import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createApp, type GenerateContent } from "./app";
import { SEEDS } from "./store";

interface TestApp {
  storePath: string;
  get: (collection: string) => Promise<Response>;
  put: (collection: string, body: unknown) => Promise<Response>;
  postAi: (body: unknown) => Promise<Response>;
  close: () => void;
}

const makeTempDir = () => fs.mkdtempSync(path.join(os.tmpdir(), "geomind-store-"));
const removeDir = (dir: string) => fs.rmSync(dir, { recursive: true, force: true });

const startApp = async (dir: string, generate: GenerateContent | null = null): Promise<TestApp> => {
  const storePath = path.join(dir, "data", "store.json");
  const server = createServer(createApp({ storePath, generate }));
  await new Promise<void>(resolve => server.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  return {
    storePath,
    get: collection => fetch(`${origin}/api/store/${collection}`),
    put: (collection, body) =>
      fetch(`${origin}/api/store/${collection}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      }),
    postAi: body =>
      fetch(`${origin}/api/gemini/generate`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      }),
    close: () => server.close(),
  };
};

test("seeds a collection and writes the store file on first read", async () => {
  const dir = makeTempDir();
  const app = await startApp(dir);
  try {
    const res = await app.get("tasks");
    assert.equal(res.status, 200);
    assert.deepEqual(await res.json(), SEEDS.tasks);
    assert.ok(fs.existsSync(app.storePath), "store.json should be created on first read");
  } finally {
    app.close();
    removeDir(dir);
  }
});

test("rejects unknown collections and path traversal", async () => {
  const dir = makeTempDir();
  const app = await startApp(dir);
  try {
    for (const name of ["secrets", "%2e%2e%2f%2e%2e%2fetc%2fpasswd", "Tasks"]) {
      assert.equal((await app.get(name)).status, 400, `GET ${name} should be rejected`);
      assert.equal((await app.put(name, [{ id: "1" }])).status, 400, `PUT ${name} should be rejected`);
    }
    assert.ok(!fs.existsSync(app.storePath), "nothing should be persisted for rejected names");
  } finally {
    app.close();
    removeDir(dir);
  }
});

test("PUT round-trips items and they survive a re-read", async () => {
  const dir = makeTempDir();
  const app = await startApp(dir);
  const items = [{ id: "t-1", title: "Written by test" }, { id: "t-2", title: "Also written" }];
  try {
    const put = await app.put("tasks", items);
    assert.equal(put.status, 200);
    assert.deepEqual(await put.json(), { ok: true, count: 2 });

    assert.deepEqual(await (await app.get("tasks")).json(), items);
    assert.deepEqual(JSON.parse(fs.readFileSync(app.storePath, "utf8")).tasks, items);
  } finally {
    app.close();
    removeDir(dir);
  }
});

test("restarting against the same file keeps saved items instead of reseeding", async () => {
  const dir = makeTempDir();
  const saved = [{ id: "keep-me", title: "Saved before restart" }];
  const first = await startApp(dir);
  try {
    await first.put("docs", saved);
  } finally {
    first.close();
  }

  const second = await startApp(dir);
  try {
    assert.deepEqual(await (await second.get("docs")).json(), saved);
  } finally {
    second.close();
    removeDir(dir);
  }
});

test("rejects bodies that are not an array of objects", async () => {
  const dir = makeTempDir();
  const app = await startApp(dir);
  const tooMany = Array.from({ length: 1001 }, (_, i) => ({ id: `t-${i}` }));
  try {
    for (const body of [{ id: "1" }, [{ title: "ok" }, null], tooMany]) {
      const res = await app.put("docs", body);
      assert.equal(res.status, 400, `${JSON.stringify(body).slice(0, 24)} should be rejected`);
    }
  } finally {
    app.close();
    removeDir(dir);
  }
});

test("AI endpoint is 503 when unconfigured and proxies when configured", async () => {
  const dir = makeTempDir();
  try {
    const offline = await startApp(dir);
    try {
      assert.equal((await offline.postAi({ prompt: "hello" })).status, 503);
    } finally {
      offline.close();
    }

    const calls: Array<{ prompt: string; systemInstruction: string }> = [];
    const online = await startApp(dir, async (prompt, systemInstruction) => {
      calls.push({ prompt, systemInstruction });
      return `echo: ${prompt}`;
    });
    try {
      assert.equal((await online.postAi({})).status, 400);

      const res = await online.postAi({ prompt: "summarise this" });
      assert.equal(res.status, 200);
      assert.deepEqual(await res.json(), { text: "echo: summarise this" });
      assert.equal(calls.length, 1);
      assert.match(calls[0].systemInstruction, /expert remote team workflow advisor/);
    } finally {
      online.close();
    }
  } finally {
    removeDir(dir);
  }
});
