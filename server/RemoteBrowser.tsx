import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type FormEvent,
  type KeyboardEvent,
  type MouseEvent,
} from "react";
import { ArrowLeft, ArrowRight, Loader2, RotateCw } from "lucide-react";

type Status = "connecting" | "live" | "closed";

type ServerMsg =
  | { type: "ready" }
  | { type: "viewport"; width: number; height: number }
  | { type: "url"; url: string }
  | { type: "title"; title: string }
  | { type: "error"; message: string };

interface Props {
  initialUrl?: string;
  onTitle?: (title: string) => void;
}

const BUTTONS = ["left", "middle", "right"] as const;

export default function RemoteBrowser({ initialUrl = "https://example.com", onTitle }: Props) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const viewport = useRef({ width: 1280, height: 720 });
  const pendingFrame = useRef<ArrayBuffer | null>(null);
  const decoding = useRef(false);
  const addressFocused = useRef(false);
  const moveRaf = useRef<number | null>(null);
  const lastMove = useRef<{ x: number; y: number } | null>(null);
  const didInitialNav = useRef(false);

  const [status, setStatus] = useState<Status>("connecting");
  const [address, setAddress] = useState(initialUrl);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  const send = useCallback((msg: object) => {
    const ws = wsRef.current;
    if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg));
  }, []);

  const sendResize = useCallback(() => {
    const el = wrapRef.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    if (r.width > 0 && r.height > 0) {
      send({ type: "resize", width: Math.round(r.width), height: Math.round(r.height) });
    }
  }, [send]);

  // Decode only the newest frame; skip any that arrived while decoding.
  const drawLatest = useCallback(async () => {
    if (decoding.current) return;
    decoding.current = true;
    try {
      while (pendingFrame.current) {
        const buf = pendingFrame.current;
        pendingFrame.current = null;
        const bmp = await createImageBitmap(new Blob([buf], { type: "image/jpeg" }));
        const c = canvasRef.current;
        if (c) {
          if (c.width !== bmp.width || c.height !== bmp.height) {
            c.width = bmp.width;
            c.height = bmp.height;
          }
          c.getContext("2d")?.drawImage(bmp, 0, 0);
        }
        bmp.close();
      }
    } catch {
      /* a corrupt frame is dropped; the next one repaints */
    } finally {
      decoding.current = false;
    }
  }, []);

  // Connection
  useEffect(() => {
    setStatus("connecting");
    setError(null);
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${proto}//${location.host}/ws/browser`);
    ws.binaryType = "arraybuffer";
    wsRef.current = ws;

    ws.onmessage = (ev) => {
      if (typeof ev.data !== "string") {
        pendingFrame.current = ev.data as ArrayBuffer;
        void drawLatest();
        return;
      }
      let msg: ServerMsg;
      try {
        msg = JSON.parse(ev.data);
      } catch {
        return;
      }
      switch (msg.type) {
        case "ready":
          setStatus("live");
          sendResize();
          break;
        case "viewport":
          viewport.current = { width: msg.width, height: msg.height };
          break;
        case "url":
          if (msg.url === "about:blank" && !didInitialNav.current) {
            didInitialNav.current = true;
            send({ type: "nav", url: initialUrl });
          } else if (!addressFocused.current && msg.url !== "about:blank") {
            setAddress(msg.url);
          }
          break;
        case "title":
          onTitle?.(msg.title);
          break;
        case "error":
          setError(msg.message);
          break;
      }
    };
    ws.onclose = () => {
      if (wsRef.current === ws) setStatus("closed");
    };

    return () => {
      ws.onclose = null;
      ws.close();
      if (wsRef.current === ws) wsRef.current = null;
    };
    // initialUrl/onTitle are read at connect time only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [attempt]);

  // Keep the remote viewport matched to the container.
  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    let t: number | undefined;
    const ro = new ResizeObserver(() => {
      window.clearTimeout(t);
      t = window.setTimeout(sendResize, 200);
    });
    ro.observe(el);
    return () => {
      window.clearTimeout(t);
      ro.disconnect();
    };
  }, [sendResize]);

  const toRemote = (clientX: number, clientY: number) => {
    const r = canvasRef.current!.getBoundingClientRect();
    return {
      x: Math.round(((clientX - r.left) * viewport.current.width) / r.width),
      y: Math.round(((clientY - r.top) * viewport.current.height) / r.height),
    };
  };

  // Wheel must be non-passive to stop the host page from scrolling.
  useEffect(() => {
    const c = canvasRef.current;
    if (!c) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const unit = e.deltaMode === 1 ? 16 : 1;
      send({ type: "wheel", ...toRemote(e.clientX, e.clientY), dx: e.deltaX * unit, dy: e.deltaY * unit });
    };
    c.addEventListener("wheel", onWheel, { passive: false });
    return () => c.removeEventListener("wheel", onWheel);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [send]);

  const onMouseMove = (e: MouseEvent<HTMLCanvasElement>) => {
    lastMove.current = toRemote(e.clientX, e.clientY);
    if (moveRaf.current !== null) return;
    moveRaf.current = requestAnimationFrame(() => {
      moveRaf.current = null;
      if (lastMove.current) send({ type: "mouse", kind: "move", ...lastMove.current });
    });
  };

  const onMouse = (kind: "down" | "up") => (e: MouseEvent<HTMLCanvasElement>) => {
    if (kind === "down") canvasRef.current?.focus();
    send({
      type: "mouse",
      kind,
      ...toRemote(e.clientX, e.clientY),
      button: BUTTONS[e.button] ?? "left",
      clicks: Math.max(1, e.detail),
    });
  };

  const onKey = (kind: "down" | "up") => (e: KeyboardEvent<HTMLCanvasElement>) => {
    // Let Ctrl/Cmd+V reach the paste handler.
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "v") return;
    if (e.key === "Dead" || e.key === "Process" || e.key === "Unidentified") return;
    e.preventDefault();
    send({ type: "key", kind, key: e.key });
  };

  const onPaste = (e: ClipboardEvent<HTMLCanvasElement>) => {
    const text = e.clipboardData.getData("text");
    if (text) {
      e.preventDefault();
      send({ type: "text", text });
    }
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    send({ type: "nav", url: address });
    canvasRef.current?.focus();
  };

  const iconBtn =
    "inline-flex h-8 w-8 items-center justify-center rounded-md text-slate-600 hover:bg-slate-100 " +
    "focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-slate-500 " +
    "disabled:opacity-40";

  return (
    <div className="flex h-full min-h-[420px] flex-col overflow-hidden rounded-lg border border-slate-200 bg-white">
      <form onSubmit={submit} className="flex items-center gap-1 border-b border-slate-200 p-2">
        <button type="button" className={iconBtn} aria-label="Back" onClick={() => send({ type: "back" })} disabled={status !== "live"}>
          <ArrowLeft size={16} />
        </button>
        <button type="button" className={iconBtn} aria-label="Forward" onClick={() => send({ type: "forward" })} disabled={status !== "live"}>
          <ArrowRight size={16} />
        </button>
        <button type="button" className={iconBtn} aria-label="Reload" onClick={() => send({ type: "reload" })} disabled={status !== "live"}>
          <RotateCw size={16} />
        </button>
        <input
          aria-label="Address or search"
          value={address}
          onChange={(e) => setAddress(e.target.value)}
          onFocus={(e) => {
            addressFocused.current = true;
            e.currentTarget.select();
          }}
          onBlur={() => (addressFocused.current = false)}
          spellCheck={false}
          autoCapitalize="off"
          autoCorrect="off"
          placeholder="Search or enter address"
          className="h-8 min-w-0 flex-1 rounded-md border border-slate-200 bg-slate-50 px-3 text-sm text-slate-900 focus:border-slate-400 focus:outline-none"
        />
      </form>

      {error && (
        <div role="alert" className="border-b border-amber-200 bg-amber-50 px-3 py-1.5 text-sm text-amber-900">
          {error}
        </div>
      )}

      <div ref={wrapRef} className="relative min-h-0 flex-1 bg-slate-100">
        <canvas
          ref={canvasRef}
          tabIndex={0}
          aria-label="Remote browser page"
          className="block h-full w-full outline-none"
          onMouseMove={onMouseMove}
          onMouseDown={onMouse("down")}
          onMouseUp={onMouse("up")}
          onContextMenu={(e) => e.preventDefault()}
          onKeyDown={onKey("down")}
          onKeyUp={onKey("up")}
          onPaste={onPaste}
        />
        {status !== "live" && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-white/80 text-sm text-slate-700">
            {status === "connecting" ? (
              <>
                <Loader2 className="animate-spin" size={20} />
                <span>Starting browser</span>
              </>
            ) : (
              <>
                <span>Disconnected from the remote browser.</span>
                <button
                  type="button"
                  onClick={() => setAttempt((n) => n + 1)}
                  className="rounded-md border border-slate-300 bg-white px-3 py-1.5 hover:bg-slate-50"
                >
                  Reconnect
                </button>
              </>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
