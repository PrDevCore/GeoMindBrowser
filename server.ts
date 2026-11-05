import { createServer as createViteServer } from "vite";
import express from "express";
import path from "path";
import dotenv from "dotenv";
import { createApp } from "./server/app";

dotenv.config();

const STORE_PATH = path.resolve(__dirname, "data", "store.json");
const app = createApp({ storePath: STORE_PATH });

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
    app.get("*", (_req, res) => {
      res.sendFile(path.resolve(__dirname, "dist", "index.html"));
    });
  }

  app.listen(Number(PORT), "0.0.0.0", () => {
    console.log(`Server running on http://localhost:${PORT}`);
    console.log(`Store: ${STORE_PATH}`);
  });
}

startServer();
