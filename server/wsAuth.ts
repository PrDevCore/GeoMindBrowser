import type { IncomingMessage } from "http";
import type { Duplex } from "stream";
import { verifyToken, findUserById, type TokenPayload } from "./auth.ts";

export function parseCookie(header: string | undefined, name: string): string | undefined {
  if (!header) return undefined;
  for (const part of header.split(";")) {
    const i = part.indexOf("=");
    if (i < 0) continue;
    if (part.slice(0, i).trim() !== name) continue;
    try {
      return decodeURIComponent(part.slice(i + 1).trim());
    } catch {
      return undefined;
    }
  }
  return undefined;
}

/**
 * Authenticates a WebSocket upgrade using the same HttpOnly `token` cookie as the REST API.
 * Also rejects cross-origin upgrades (cross-site WebSocket hijacking).
 * If you run behind a reverse proxy, make sure it forwards the original Host header.
 */
export function authenticateUpgrade(req: IncomingMessage): TokenPayload | null {
  const origin = req.headers.origin;
  if (origin) {
    try {
      if (new URL(origin).host !== req.headers.host) return null;
    } catch {
      return null;
    }
  }
  const token = parseCookie(req.headers.cookie, "token");
  if (!token) return null;
  const payload = verifyToken(token);
  if (!payload) return null;
  return findUserById(payload.userId) ? payload : null;
}

export function rejectUpgrade(socket: Duplex, status = 401, text = "Unauthorized") {
  socket.write(`HTTP/1.1 ${status} ${text}\r\nConnection: close\r\nContent-Length: 0\r\n\r\n`);
  socket.destroy();
}
