import { useCallback, useEffect, useRef, useState } from "react";
import { apiJson } from "./api";

export type SaveState = "idle" | "saving" | "saved" | "error";

const SYNC_DEBOUNCE_MS = 400;

export interface StoredList<T> {
  items: T[];
  update: (next: T[]) => void;
  flush: () => Promise<void>;
  saveState: SaveState;
  error: string | null;
}

export function useStoredList<T>(collection: string, seed: T[]): StoredList<T> {
  const [items, setItems] = useState<T[]>(seed);
  const [saveState, setSaveState] = useState<SaveState>("idle");
  const [error, setError] = useState<string | null>(null);

  const endpoint = `/api/${collection}`;
  const latest = useRef(items);
  latest.current = items;
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await apiJson(endpoint, "GET");
        if (cancelled) return;
        if (Array.isArray(data)) setItems(data);
      } catch (err: any) {
        if (!cancelled) setError(err.message || "Failed to load");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [endpoint]);

  useEffect(() => {
    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
  }, []);

  const write = useCallback(async () => {
    setSaveState("saving");
    try {
      await apiJson(endpoint, "PUT", latest.current);
      setSaveState("saved");
      setError(null);
    } catch (err: any) {
      const message = err instanceof Error ? err.message : "Failed to save";
      setSaveState("error");
      setError(message);
    }
  }, [endpoint]);

  const update = useCallback((next: T[]) => {
    latest.current = next;
    setItems(next);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(write, SYNC_DEBOUNCE_MS);
  }, [write]);

  const flush = useCallback(async () => {
    if (timer.current) clearTimeout(timer.current);
    await write();
  }, [write]);

  return { items, update, flush, saveState, error };
}
