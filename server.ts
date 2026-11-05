import { createServer as createViteServer } from "vite";
import express from "express";
import path from "path";
import dotenv from "dotenv";
import { GoogleGenAI } from "@google/genai";
import { createApp } from "./server/app";

dotenv.config();

const STORE_PATH = path.resolve(__dirname, "data", "store.json");
const GEMINI_MODEL = process.env.GEMINI_MODEL || "gemini-3.8-flash";

const apiKey = process.env.GEMINI_API_KEY;
if (!apiKey) {
  console.warn("[GeoMind] GEMINI_API_KEY is not set. The AI copilot is disabled until you add a key to .env");
}

const ai = apiKey
  ? new GoogleGenAI({
      apiKey,
      httpOptions: {
        headers: {
          "User-Agent": "aistudio-build",
        },
      },
    })
  : null;

const app = createApp({
  storePath: STORE_PATH,
  generate: ai
    ? async (prompt, systemInstruction) => {
        const response = await ai.models.generateContent({
          model: GEMINI_MODEL,
          contents: prompt,
          config: { systemInstruction },
        });
        return response.text || "No response generated.";
      }
    : null,
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
