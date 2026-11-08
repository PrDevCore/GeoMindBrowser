#!/usr/bin/env node
// Usage: node apply_vpnhub_edits.js <path/to/VpnHub.tsx> [--dry-run]
// Every anchor must match exactly once or the script aborts without writing.
const fs = require('fs');

const target = process.argv[2];
const dryRun = process.argv.includes('--dry-run');
if (!target) {
  console.error('Usage: node apply_vpnhub_edits.js <path/to/VpnHub.tsx> [--dry-run]');
  process.exit(2);
}

const raw = fs.readFileSync(target, 'utf8');
const eol = raw.includes('\r\n') ? '\r\n' : '\n';
let src = raw.replace(/\r\n/g, '\n');

const edits = [
  {
    name: 'props: vpnLocations becomes optional/deprecated',
    from: `  vpnLocations: VpnLocation[];
  activeVpnLocation: VpnLocation | null;`,
    to: `  /** @deprecated Gateways are now fetched from /api/vpn-locations inside this component. */
  vpnLocations?: VpnLocation[];
  activeVpnLocation: VpnLocation | null;`,
  },
  {
    name: 'destructure: drop unused vpnLocations',
    from: `  setVpnEnabled,
  vpnLocations,
  activeVpnLocation,`,
    to: `  setVpnEnabled,
  activeVpnLocation,`,
  },
  {
    name: 'state: add gatewayLocations',
    from: `  const [locationsError, setLocationsError] = useState<string | null>(null);
`,
    to: `  const [locationsError, setLocationsError] = useState<string | null>(null);
  const [gatewayLocations, setGatewayLocations] = useState<VpnLocation[]>([]);
`,
  },
  {
    name: 'fetch: keep the backend response',
    from: `        if (Array.isArray(data)) {
          if (data.length > 0) {
            console.log('Fetched VPN locations from backend:', data.length);
          }
        }`,
    to: `        if (!Array.isArray(data)) {
          throw new Error('Unexpected response from /api/vpn-locations');
        }
        setGatewayLocations(data);`,
  },
  {
    name: 'notice: reflect whether gateways exist',
    from: `            <span className="font-bold">No gateway data.</span> The backend exposes no gateway inventory, so there is nothing to show here. No tunnel is created, no traffic is encrypted or rerouted. Use your real VPN client for actual protection.`,
    to: `            {gatewayLocations.length === 0 ? (
              <>
                <span className="font-bold">No gateway data.</span> The backend returned no gateways, so there is nothing to select. No tunnel is created, no traffic is encrypted or rerouted. Use your real VPN client for actual protection.
              </>
            ) : (
              <>
                <span className="font-bold">Simulation only.</span> Gateways are loaded from the backend, but no tunnel is created and no traffic is encrypted or rerouted. Use your real VPN client for actual protection.
              </>
            )}`,
  },
  {
    name: 'start button: use fetched gateways',
    from: `              if (!vpnEnabled && !activeVpnLocation && vpnLocations.length > 0) {
                setActiveVpnLocation(vpnLocations[0]);
              }`,
    to: `              if (!vpnEnabled && !activeVpnLocation && gatewayLocations.length > 0) {
                setActiveVpnLocation(gatewayLocations[0]);
              }`,
  },
  {
    name: 'selector: render fetched gateways',
    from: `            {vpnLocations.map(loc => {`,
    to: `            {gatewayLocations.map(loc => {`,
  },
  {
    name: 'selector: empty state',
    from: `            })}
          </div>

          <div className="flex items-center gap-2 text-[11px]`,
    to: `            })}
            {gatewayLocations.length === 0 && (
              <p className="md:col-span-2 text-xs text-slate-500 dark:text-slate-400">
                No gateways available from the backend.
              </p>
            )}
          </div>

          <div className="flex items-center gap-2 text-[11px]`,
  },
];

const count = (hay, needle) => hay.split(needle).length - 1;

for (const e of edits) {
  const n = count(src, e.from);
  if (n !== 1) {
    console.error(`ABORT: anchor "${e.name}" matched ${n} times (expected 1). Nothing written.`);
    process.exit(1);
  }
}
for (const e of edits) {
  src = src.replace(e.from, () => e.to);
  console.log(`ok  ${e.name}`);
}

if (dryRun) {
  console.log('Dry run: no file written.');
} else {
  fs.writeFileSync(target, src.replace(/\n/g, eol), 'utf8');
  console.log(`Wrote ${target}`);
}