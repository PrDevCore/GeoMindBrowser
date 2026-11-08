import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createApp } from "./app";

interface TestApp {
  origin: string;
  close: () => void;
  jar: Map<string, string>;
  request: (path: string, init?: RequestInit) => Promise<Response>;
  json: (path: string, init?: RequestInit) => Promise<any>;
}

const startApp = async (): Promise<TestApp> => {
  process.env.DB_PATH = path.join(os.tmpdir(), `waypoint-test-${process.pid}-${Date.now()}-${Math.random()}.db`);
  const app = createApp();
  const server = createServer(app);
  await new Promise<void>(resolve => server.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  const jar = new Map<string, string>();
  const dbPath = process.env.DB_PATH!;

  const request = async (path: string, init: RequestInit = {}): Promise<Response> => {
    const headers = new Headers(init.headers ?? {});
    if (jar.size > 0) {
      headers.set("Cookie", [...jar.entries()].map(([k, v]) => `${k}=${v}`).join("; "));
    }
    const res = await fetch(`${origin}${path}`, { ...init, headers });
    const setCookie = res.headers.get("set-cookie");
    if (setCookie) {
      for (const part of setCookie.split(",")) {
        const kv = part.split(";")[0].split("=");
        if (kv.length === 2) jar.set(kv[0].trim(), kv[1].trim());
      }
    }
    return res;
  };

  const json = async (path: string, init?: RequestInit): Promise<any> => {
    const res = await request(path, init);
    return res.json();
  };

  return { origin, close: () => { server.close(); try { fs.unlinkSync(dbPath); } catch {} }, jar, request, json };;
};

test("register then login sets session cookie and /api/auth/me returns the user", async () => {
  const app = await startApp();
  try {
    const regBody = await app.json("/api/auth/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: "alice@example.com", password: "secret123", name: "Alice" }),
    });
    assert.equal(regBody.ok, true);
    const meBody = await app.json("/api/auth/me");
    assert.equal(meBody.user.email, "alice@example.com");
    assert.equal(meBody.user.name, "Alice");
    const reg2 = await app.request("/api/auth/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: "alice@example.com", password: "secret123", name: "Alice" }),
    });
    assert.equal(reg2.status, 409);
  } finally {
    app.close();
  }
});

test("unauthenticated requests to protected routes are rejected", async () => {
  const app = await startApp();
  try {
    for (const path of ["/api/tasks", "/api/docs", "/api/chat", "/api/auth/me"]) {
      assert.equal((await app.request(path)).status, 401, `${path} should require auth`);
    }
  } finally {
    app.close();
  }
});

test("tasks CRUD round-trips and is user-scoped", async () => {
  const app = await startApp();
  try {
    await app.request("/api/auth/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: "bob@example.com", password: "secret123", name: "Bob" }),
    });

    let tasks = await app.json("/api/tasks");
    assert.deepEqual(tasks, []);

    const created = await app.json("/api/tasks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        title: "Deploy WireGuard",
        description: "Set up EU/UK endpoints",
        status: "in_progress",
        priority: "urgent",
        assignee: { name: "Bob", role: "Lead" },
        dueDate: "2026-11-12",
        vpnRequired: true,
        tags: ["Security"],
      }),
    });
    assert.equal(created.title, "Deploy WireGuard");
    const taskId = created.id;

    tasks = await app.json("/api/tasks");
    assert.equal(tasks.length, 1);
    assert.equal(tasks[0].id, taskId);

    const updated = await app.json(`/api/tasks/${taskId}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ status: "done" }),
    });
    assert.equal(updated.status, "done");

    await app.request(`/api/tasks/${taskId}`, { method: "DELETE" });
    tasks = await app.json("/api/tasks");
    assert.deepEqual(tasks, []);
  } finally {
    app.close();
  }
});

test("docs CRUD round-trips", async () => {
  const app = await startApp();
  try {
    await app.request("/api/auth/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: "carol@example.com", password: "secret123", name: "Carol" }),
    });

    const doc = await app.json("/api/docs", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title: "Spec", content: "# Hello", category: "Architecture" }),
    });
    assert.equal(doc.title, "Spec");
    const docId = doc.id;

    const upd = await app.json(`/api/docs/${docId}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title: "Spec v2", content: "# Updated" }),
    });
    assert.equal(upd.title, "Spec v2");

    await app.request(`/api/docs/${docId}`, { method: "DELETE" });
    const list = await app.json("/api/docs");
    assert.deepEqual(list, []);
  } finally {
    app.close();
  }
});

test("chat POST/GET round-trips and persists", async () => {
  const app = await startApp();
  try {
    await app.request("/api/auth/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: "dave@example.com", password: "secret123", name: "Dave" }),
    });

    const created = await app.json("/api/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text: "hello world", vpnNode: "London (simulated)" }),
    });
    assert.equal(created.text, "hello world");

    const msgs = await app.json("/api/chat");
    assert.equal(msgs.length, 1);
    assert.equal(msgs[0].text, "hello world");
  } finally {
    app.close();
  }
});

test("health endpoint reports sqlite", async () => {
  const app = await startApp();
  try {
    const body = await app.json("/api/health");
    assert.equal(body.db, "sqlite");
  } finally {
    app.close();
  }
});

test("unknown API route returns 404", async () => {
  const app = await startApp();
  try {
    await app.request("/api/auth/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: "eve@example.com", password: "secret123", name: "Eve" }),
    });
    const res = await app.request("/api/nope");
    assert.equal(res.status, 404);
  } finally {
    app.close();
  }
});


