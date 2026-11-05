import { useCallback, useEffect, useRef, useState } from 'react';

export type SaveState = 'idle' | 'saving' | 'saved' | 'error';

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
  const [saveState, setSaveState] = useState<SaveState>('idle');
  const [error, setError] = useState<string | null>(null);

  const endpoint = `/api/store/${collection}`;
  const latest = useRef(items);
  latest.current = items;
  const localEdit = useRef(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetch(endpoint)
      .then(res => res.ok ? res.json() : Promise.reject(new Error(`Store read failed (${res.status})`)))
      .then(data => {
        if (cancelled) return;
        if (Array.isArray(data) && !localEdit.current) setItems(data);
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message);
      });
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
    setSaveState('saving');
    try {
      const res = await fetch(endpoint, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(latest.current),
      });
      if (!res.ok) throw new Error(`Store write failed (${res.status})`);
      setSaveState('saved');
      setError(null);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to save';
      setSaveState('error');
      setError(message);
    }
  }, [endpoint]);

  const update = useCallback((next: T[]) => {
    localEdit.current = true;
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
