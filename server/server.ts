import { createServer as createViteServer } from "vite";
import express from "express";
import { createServer as createHttpServer } from "http";
import path from "path";
import dotenv from "dotenv";
import { createApp } from "./app";
import { handleChatUpgrade } from "./ws";
import { handleBrowserUpgrade, shutdownBrowser } from "./browser";

dotenv.config();

const app = createApp();
const httpServer = createHttpServer(app);

// One router for all WebSocket upgrades. Each endpoint uses `noServer: true`,
// because two `ws` servers with a `path` option on the same HTTP server
// reject each other's connections.
httpServer.on("upgrade", (req, socket, head) => {
  const { pathname } = new URL(req.url ?? "/", "http://localhost");
  if (pathname === "/ws") handleChatUpgrade(req, socket, head);
  else if (pathname === "/ws/browser") handleBrowserUpgrade(req, socket, head);
  else if (process.env.NODE_ENV === "production") socket.destroy();
  // In dev, leave other paths alone so Vite HMR still works if you re-enable it.
});

async function startServer() {
  const PORT = process.env.PORT || 3000;

  if (process.env.NODE_ENV !== "production") {
    const vite = await createViteServer({
      server: { middlewareMode: true },
      appType: "spa",
    });
    app.use(vite.middlewares);
  } else {
    app.use(express.static(path.resolve(__dirname, "dist")));
    app.get("*", (_req: express.Request, res: express.Response) => {
      res.sendFile(path.resolve(__dirname, "dist", "index.html"));
    });
  }

  httpServer.listen(Number(PORT), "0.0.0.0", () => {
    console.log(`Server running on http://localhost:${PORT}`);
    console.log(`WebSocket at ws://localhost:${PORT}/ws`);
    console.log(`Remote browser at ws://localhost:${PORT}/ws/browser`);
  });
}

for (const sig of ["SIGINT", "SIGTERM"] as const) {
  process.on(sig, () => {
    void shutdownBrowser().finally(() => process.exit(0));
  });
}

startServer();
