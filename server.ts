import { createServer as createViteServer } from "vite";
import express from "express";
import { createServer as createHttpServer } from "http";
import path from "path";
import dotenv from "dotenv";
import { createApp } from "./server/app";
import { setupWebSocket } from "./server/ws";

dotenv.config();

const app = createApp();
const httpServer = createHttpServer(app);

setupWebSocket(httpServer);

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
    app.get("*", ({ url }: { url: string }, res: express.Response) => {
      res.sendFile(path.resolve(__dirname, "dist", "index.html"));
    });
  }

  httpServer.listen(Number(PORT), "0.0.0.0", () => {
    console.log(`Server running on http://localhost:${PORT}`);
    console.log(`WebSocket at ws://localhost:${PORT}/ws`);
  });
}

startServer();
