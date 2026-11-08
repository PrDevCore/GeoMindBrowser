const API_BASE = import.meta.env.VITE_API_BASE ?? "";

type Method = "GET" | "POST" | "PUT" | "DELETE";

async function request(path: string, method: Method = "GET", body?: unknown): Promise<Response> {
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
    credentials: "include",
  });
  return res;
}

export async function apiJson<T = any>(path: string, method: Method = "GET", body?: unknown): Promise<T> {
  const res = await request(path, method, body);
  if (res.status === 401) {
    const err: any = new Error("Unauthorized");
    err.status = 401;
    throw err;
  }
  if (!res.ok) {
    let msg = `Request failed (${res.status})`;
    try {
      const data = await res.json();
      msg = data.error || msg;
    } catch {}
    const err: any = new Error(msg);
    err.status = res.status;
    throw err;
  }
  if (res.status === 204) return null as unknown as T;
  return res.json();
}

export { request };