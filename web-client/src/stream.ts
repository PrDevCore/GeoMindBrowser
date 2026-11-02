import { isStreaming, type SessionView } from "./session.js";

export type StreamLink = {
  href: string;
  target: "_blank";
  rel: "noreferrer noopener";
  referrerPolicy: "no-referrer";
};

// A stream URL is bearer-equivalent for the session view, so it must not reach
// the token, the page history as a labelled link, or any referrer.
export function streamLink(view: SessionView | null): StreamLink | null {
  if (view === null || !isStreaming(view.state)) return null;
  const url = view.streamUrl;
  if (url === undefined || !url.startsWith("https://")) return null;
  return {
    href: url,
    target: "_blank",
    rel: "noreferrer noopener",
    referrerPolicy: "no-referrer",
  };
}

export function leaksToken(link: StreamLink, token: string): boolean {
  if (token === "") return false;
  return link.href.includes(token) || link.href.includes(encodeURIComponent(token));
}
