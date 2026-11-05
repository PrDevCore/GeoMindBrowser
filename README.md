# GeoMind Browser & Team Workspace

React workspace demo: an embedded browser panel, sprint board, shared documents and team chat
backed by a local Express server.

## Run Locally

**Prerequisites:** Node.js

1. Install dependencies:
   `npm install`
2. Run the app:
   `npm run dev`

Optional: copy `.env.example` to `.env` to set `PORT` (defaults to 3000).

## Checks

- `npm run typecheck` - typecheck with `tsc --noEmit`
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

The VPN Suite, the gateway inventory (IPs, ping, load) and the team directory are simulated
demo content: no tunnel is created and no traffic is encrypted or rerouted. The browser panel
embeds pages in an iframe, so sites that forbid framing render blank.
