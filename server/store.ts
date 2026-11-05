import fs from "fs";
import path from "path";
import { SEED_CHAT, SEED_DOCS, SEED_TASKS } from "../src/data/seed";

// data/store.json is the source of truth once written; src/data/seed.ts only
// provides the initial sample content for a collection that has never been saved.
export const SEEDS: Record<string, unknown[]> = {
  tasks: SEED_TASKS,
  docs: SEED_DOCS,
  chat: SEED_CHAT,
};

export const MAX_ITEMS = 1000;

export const isCollection = (name: string): boolean =>
  Object.prototype.hasOwnProperty.call(SEEDS, name);

export interface JsonStore {
  read(): Record<string, unknown[]>;
  write(collections: Record<string, unknown[]>): void;
}

export const createJsonStore = (storePath: string): JsonStore => {
  const write = (collections: Record<string, unknown[]>) => {
    fs.mkdirSync(path.dirname(storePath), { recursive: true });
    fs.writeFileSync(storePath, JSON.stringify(collections, null, 2), "utf8");
  };

  const read = (): Record<string, unknown[]> => {
    let existing: Record<string, unknown[]> = {};
    if (fs.existsSync(storePath)) {
      const raw = fs.readFileSync(storePath, "utf8");
      try {
        existing = JSON.parse(raw);
      } catch {
        throw new Error(`${storePath} is not valid JSON - repair or delete it and restart.`);
      }
      if (!existing || typeof existing !== "object") {
        throw new Error(`${storePath} must contain a JSON object of collections.`);
      }
    }

    let initialized = false;
    for (const [name, seed] of Object.entries(SEEDS)) {
      if (!Array.isArray(existing[name])) {
        existing[name] = seed;
        initialized = true;
      }
    }
    if (initialized || !fs.existsSync(storePath)) write(existing);
    return existing;
  };

  return { read, write };
};
