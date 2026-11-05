# GeoMind workspace demo

A React + Express UI mock of a team workspace: an embedded browser panel, a sprint board, shared
documents and team chat. It is kept here as a design reference for the workspace surfaces the
extension and companion will eventually serve.

**It is not the Phase 1 client.** Nothing in this directory routes traffic. The VPN Suite, the
gateway inventory (IPs, ping, load) and the team directory are simulated sample content: no
tunnel is created and no traffic is encrypted or rerouted. There is no AI feature.

## Run locally

**Prerequisites:** Node.js

1. Install dependencies: `npm install`
2. Run the app: `npm run dev`

Optional: copy `.env.example` to `.env` to set `PORT` (defaults to 3000).

## Checks

- `npm run typecheck` - `tsc --noEmit`
- `npm test` - `node:test` suite over the store routes (`server/app.test.ts`)
- `npm run build` - production bundle in `dist/`

## Data, persistence and what is simulated

Tasks, documents and chat messages persist to `data/store.json` through
`GET`/`PUT /api/store/:collection` (`tasks`, `docs`, `chat`). Delete that file to go back to
the sample content in `src/data/seed.ts`. The HTTP routes live in `server/app.ts`, the file
store in `server/store.ts`, and `server.ts` is the entry point that wires them to Vite or `dist/`.

Theme choices (accent colour and background preset) are kept per browser in `localStorage` under
`geomind.theme`; the accent is applied as `--brand-*` custom properties that the `brand-*`
Tailwind utilities read.

No browsing data is persisted anywhere. The browser panel keeps its tabs in component state only,
there is no telemetry or analytics code, and nothing is sent off the machine except the
same-origin `/api/store` calls.
