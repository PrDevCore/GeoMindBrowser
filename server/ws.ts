import { WebSocketServer, type WebSocket as WS } from "ws";
import type { IncomingMessage } from "http";
import type { Duplex } from "stream";
import { authenticateUpgrade, rejectUpgrade } from "./wsAuth";

const wss = new WebSocketServer({ noServer: true });
const clients = new Map<string, Set<WS>>();
// Track who is currently connected so presence updates can be broadcast.
const online = new Set<string>();

/** Called from the central `upgrade` router in server.ts for path /ws. */
export function handleChatUpgrade(req: IncomingMessage, socket: Duplex, head: Buffer) {
  // Identity comes from the signed JWT cookie, not from a client-supplied ?userId=.
  const user = authenticateUpgrade(req);
  if (!user) {
    rejectUpgrade(socket);
    return;
  }
  wss.handleUpgrade(req, socket, head, (ws) => onConnection(ws, user.userId));
}

function onConnection(ws: WS, userId: string) {
  if (!clients.has(userId)) clients.set(userId, new Set());
  clients.get(userId)!.add(ws);
  online.add(userId);

  // Tell everyone (including this user) that a user came online.
  broadcastAll({ type: "presence", online: [...online] });

  ws.on("close", () => {
    const set = clients.get(userId);
    set?.delete(ws);
    if (set?.size === 0) {
      clients.delete(userId);
      online.delete(userId);
      broadcastAll({ type: "presence", online: [...online] });
    }
  });
}

export function broadcast(userId: string, payload: unknown) {
  const msg = JSON.stringify(payload);
  const set = clients.get(userId);
  if (!set) return;
  for (const client of set) {
    if (client.readyState === 1) client.send(msg);
  }
}

export function broadcastAll(payload: unknown) {
  const msg = JSON.stringify(payload);
  for (const set of clients.values()) {
    for (const client of set) {
      if (client.readyState === 1) client.send(msg);
    }
  }
}

/**
 * Attaches the WebSocket upgrade handler to an HTTP server.
 * Routes /ws → chat, /ws/browser → remote browser.
 */
export function setupWebSocket(httpServer: import("http").Server) {
  httpServer.on("upgrade", (req, socket, head) => {
    const { pathname } = new URL(req.url ?? "/", "http://localhost");
    if (pathname === "/ws") {
      handleChatUpgrade(req, socket, head);
    } else if (pathname === "/ws/browser") {
      // Browser upgrade is handled by server/browser.ts
      socket.destroy();
    } else if (process.env.NODE_ENV === "production") {
      socket.destroy();
    }
    // In dev, leave other paths alone.
  });
}
