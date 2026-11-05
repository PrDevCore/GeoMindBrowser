import express, { Express, Request, Response } from "express";
import { createJsonStore, isCollection, MAX_ITEMS } from "./store";

export interface AppOptions {
  storePath: string;
}

export const createApp = ({ storePath }: AppOptions): Express => {
  const app = express();
  const store = createJsonStore(storePath);
  app.use(express.json({ limit: "1mb" }));

  app.get("/api/store/:collection", (req: Request, res: Response) => {
    const { collection } = req.params;
    if (!isCollection(collection)) {
      res.status(400).json({ error: `Unknown collection "${collection}".` });
      return;
    }
    try {
      res.json(store.read()[collection]);
    } catch (error: any) {
      console.error("Store read error:", error);
      res.status(500).json({ error: error.message || "Failed to read the store" });
    }
  });

  app.put("/api/store/:collection", (req: Request, res: Response) => {
    const { collection } = req.params;
    if (!isCollection(collection)) {
      res.status(400).json({ error: `Unknown collection "${collection}".` });
      return;
    }
    const items = req.body;
    if (!Array.isArray(items) || items.length > MAX_ITEMS || items.some(i => i === null || typeof i !== "object")) {
      res.status(400).json({ error: `Expected a JSON array of up to ${MAX_ITEMS} objects.` });
      return;
    }
    try {
      const collections = store.read();
      collections[collection] = items;
      store.write(collections);
      res.json({ ok: true, count: items.length });
    } catch (error: any) {
      console.error("Store write error:", error);
      res.status(500).json({ error: error.message || "Failed to write the store" });
    }
  });

  return app;
};
