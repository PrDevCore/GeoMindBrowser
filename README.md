# Waypoint Browser & Team Workspace

A real, full-stack workspace: React + Vite front-end, Express + SQLite backend,
JWT authentication, RESTful resource API, and live WebSocket chat.

## What actually runs

- **Data is persisted in SQLite** (`data/app.db`) — tasks, documents, chat messages,
  and users all live in a real database, not JSON files. `server/db.ts` manages the
  schema, migrations, and row mapping.
- **Authentication is real**: register, login, logout, and session claims via JWT.
  The session cookie is `HttpOnly`, so it is handled server-side.
- **API is RESTful**: `POST /api/auth/register|login|logout`, `GET /api/auth/me`,
  plus `/api/tasks`, `/api/docs`, and `/api/chat` — all endpoints are **user-scoped**
  (data is isolated per authenticated user).
- **Chat is real-time**: a WebSocket (`ws://localhost:3000/ws?userId=<id>`) pushes
  `chat:new` messages so the team chat stream updates without polling.
- **VPN/Team/Browser panels are still demo UI** — the VPN console, pings, loads, team
  presence, and embedded sites are illustrative mock-ups, as documented in the
  individual component files and the notes below.

## Run locally

Prerequisites: Node.js 20+.

```bash
npm install
npm run dev          # -> http://localhost:3000
```

Open http://localhost:3000 and sign up. The first account created is the only one
that will share the local SQLite store.

### Checks

| Command | What it does |
|---------|--------------|
| `npm run lint` | `tsc --noEmit` typecheck (front + back) |
| `npm test` | `node:test` suite against the Express app (auth, auth-scoped CRUD, chat, 404s) |
| `npm run build` | `vite build` -> `dist/` (served in production) |
| `npm start` | `node server.ts` production mode (serves `dist/`) |

## Architecture

```
â”Œâ”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”     â”Œâ”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”     â”Œâ”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”
â”‚   Browser    â”‚â”€â”€â”€â”€â–¶â”‚  Vite +      â”‚â”€â”€â”€â”€â–¶â”‚  Express     â”‚
â”‚   (React)    â”‚     â”‚  TS (front)  â”‚     â”‚  + TS (back) â”‚
â””â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”˜     â””â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”˜     â””â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”˜
       â”‚                                                    â”‚
       â”‚  HTTPS                                           â”‚  HTTP
       â–¼                                                    â–¼
â”Œâ”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”
â”‚                    HTTP Server                              â”‚
â”‚  â”Œâ”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”    â”Œâ”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”             â”‚
â”‚  â”‚ REST API         â”‚    â”‚ WebSocket (ws)   â”‚             â”‚
â”‚  â”‚ /api/auth/*      â”‚â—€â”€â”€â–¶â”‚ /ws  (chat)     â”‚             â”‚
â”‚  â”‚ /api/tasks/*     â”‚    â”‚ /ws/browser (Playwright)          â”‚
â”‚  â”‚ /api/docs/*      â”‚    â””â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”˜             â”‚
â”‚  â”‚ /api/chat/*      â”‚                                     â”‚
â”‚  â””â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”˜                                     â”‚
â”‚  â”Œâ”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”                                     â”‚
â”‚  â”‚ SQLite           â”‚  (better-sqlite3, WAL mode)         â”‚
â”‚  â”‚ users · tasks ·  â”‚                                     â”‚
â”‚  â”‚ docs · chat_msgs â”‚                                     â”‚
â”‚  â””â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”˜                                     â”‚
â””â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”˜
```

## Data, persistence, and what is simulated

- **Source of truth = SQLite.** `server/db.ts` creates and initializes the database
  with tables for `users`, `tasks`, `docs`, and `chat_messages`. Each table is
  indexed by `user_id` so data is isolated per authenticated user.
- **Authentication** uses `jsonwebtoken` (JWT, 7-day expiry) and `bcryptjs`
  (password hashing, cost factor 10). Tokens are stored in an `HttpOnly` cookie.
- The VPN suite, gateway inventory (IPs, ping, load), team presence, and the browser
  panel's embedded sites are **simulated demo content**: no tunnel is created, no
  traffic is encrypted or rerouted, and sites that forbid framing render blank in
  the iframe.

## Theme choices

Accent colour and background preset are kept per browser in `localStorage` under
`waypoint.theme`; the accent is applied as `--brand-*` custom properties that the
`brand-*` Tailwind utilities read.



