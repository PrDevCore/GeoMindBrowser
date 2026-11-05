<div align="center">
<img width="1200" height="475" alt="GHBanner" src="https://ai.google.dev/static/site-assets/images/share-ais-513315318.png" />
</div>

# Run and deploy your AI Studio app

This contains everything you need to run your app locally.

View your app in AI Studio: https://ai.studio/apps/0af9f7f8-e92f-4630-adc1-f1428093c6ae

## Run Locally

**Prerequisites:**  Node.js


1. Install dependencies:
   `npm install`
2. Copy `.env.example` to `.env` and set `GEMINI_API_KEY` to your Gemini API key
3. Run the app:
   `npm run dev`

Without a key the server still runs; the AI copilot returns a 503 until one is added.
Set `GEMINI_MODEL` in `.env` to override the default model (`gemini-3.8-flash`).

## Checks

- `npm run lint` - typecheck with `tsc --noEmit`
- `npm test` - `node:test` suite over the store and AI proxy routes (`server/app.test.ts`)
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
