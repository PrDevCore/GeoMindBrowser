import express, { Express, Request, Response } from "express";
import { createJsonStore, isCollection, MAX_ITEMS } from "./store";

export type GenerateContent = (prompt: string, systemInstruction: string) => Promise<string>;

export interface AppOptions {
  storePath: string;
  generate?: GenerateContent | null;
}

export const DEFAULT_SYSTEM_INSTRUCTION =
  "You are an expert remote team workflow advisor, enterprise browser copilot, and cybersecurity analyst.";

export const createApp = ({ storePath, generate = null }: AppOptions): Express => {
  const app = express();
  const store = createJsonStore(storePath);
  app.use(express.json({ limit: "1mb" }));

  app.post("/api/gemini/generate", async (req: Request, res: Response) => {
    if (!generate) {
      res.status(503).json({ error: "AI is not configured: set GEMINI_API_KEY in .env and restart the server." });
      return;
    }
    const { prompt, systemInstruction } = req.body ?? {};
    if (!prompt) {
      res.status(400).json({ error: "Prompt is required" });
      return;
    }
    try {
      res.json({ text: await generate(prompt, systemInstruction || DEFAULT_SYSTEM_INSTRUCTION) });
    } catch (error: any) {
      console.error("Gemini API error:", error);
      res.status(500).json({ error: error.message || "Failed to generate AI response" });
    }
  });

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
