import { useEffect } from "react";

export type ChatSocketMessage =
  | { type: "chat:new"; message: { id: string; senderName: string; text: string; timestamp: number; vpnNode: string } }
  | { type: "presence"; online: string[] };

export function useChatSocket(userId: string | null, onMessage: (msg: ChatSocketMessage) => void) {
  useEffect(() => {
    if (!userId) return;
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${proto}//${window.location.host}/ws?userId=${encodeURIComponent(userId)}`);
    ws.onmessage = (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data) as ChatSocketMessage;
        if (data.type === "chat:new" || data.type === "presence") onMessage(data);
      } catch {
        // Ignore malformed messages
      }
    };
    return () => {
      ws.close();
    };
  }, [userId, onMessage]);
}