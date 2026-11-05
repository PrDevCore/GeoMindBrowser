import React from 'react';
import { VpnLocation } from '../types';
import { AlertTriangle, ShieldCheck, ShieldAlert } from 'lucide-react';

interface VpnHubProps {
  vpnEnabled: boolean;
  setVpnEnabled: (enabled: boolean) => void;
  vpnLocations: VpnLocation[];
  activeVpnLocation: VpnLocation;
  setActiveVpnLocation: (loc: VpnLocation) => void;
}

export const VpnHub: React.FC<VpnHubProps> = ({
  vpnEnabled,
  setVpnEnabled,
  vpnLocations,
  activeVpnLocation,
  setActiveVpnLocation,
}) => {
  return (
    <div className="flex flex-col h-[calc(100vh-65px)] bg-slate-50 dark:bg-slate-950 text-slate-900 dark:text-slate-100 p-8 overflow-y-auto">
      <div className="max-w-4xl mx-auto w-full space-y-8">
        {/* Simulation Notice */}
        <div className="flex items-start gap-3 p-4 rounded-2xl border border-amber-300 dark:border-amber-700/60 bg-amber-50 dark:bg-amber-950/30 text-amber-800 dark:text-amber-300">
          <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" />
          <div className="text-xs leading-relaxed">
            <span className="font-bold">Simulated console.</span> This panel is a UI mock-up. No tunnel is created, no traffic is encrypted or rerouted, and every
            address, ping, load and status below is sample data from <span className="font-mono">src/data/seed.ts</span>. Use your real VPN client for actual protection.
          </div>
        </div>

        {/* Header */}
        <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4 p-6 rounded-2xl bg-gradient-to-r from-brand-900 via-slate-900 to-brand-950 text-white shadow-xl">
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <span className={`px-2.5 py-1 rounded-md text-xs font-mono font-medium ${vpnEnabled ? 'bg-emerald-500/20 text-emerald-400' : 'bg-red-500/20 text-red-400'}`}>
                {vpnEnabled ? 'Simulation ON' : 'Simulation OFF'}
              </span>
              <span className="px-2.5 py-1 rounded-md bg-blue-500/20 text-blue-300 text-xs font-mono">
                Protocol label: WireGuard v1.0 (illustrative)
              </span>
            </div>
            <h2 className="text-2xl font-bold tracking-tight">VPN Gateway Console (Demo)</h2>
            <p className="text-sm text-slate-300">
              A mock-up of an enterprise VPN control panel, useful for reviewing the interface and flow.
            </p>
          </div>

          <button
            onClick={() => setVpnEnabled(!vpnEnabled)}
            className={`px-6 py-3 rounded-xl font-bold text-sm shadow-lg transition-all ${
              vpnEnabled
                ? 'bg-emerald-600 hover:bg-emerald-500 text-white shadow-emerald-500/20'
                : 'bg-slate-700 hover:bg-slate-600 text-white'
            }`}
          >
            {vpnEnabled ? 'Stop Simulation' : 'Start Simulation'}
          </button>
        </div>

        {/* Status Grid */}
        <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
          <div className="p-5 rounded-2xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-sm">
            <div className="text-xs text-slate-500 dark:text-slate-400 font-medium">Sample Client Address</div>
            <div className="text-lg font-mono font-bold text-slate-900 dark:text-white mt-1">
              {vpnEnabled ? activeVpnLocation.ip : '192.168.1.10'}
            </div>
            <div className="text-[11px] text-slate-500 dark:text-slate-400 mt-2 font-medium">Hardcoded value · never measured</div>
          </div>

          <div className="p-5 rounded-2xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-sm">
            <div className="text-xs text-slate-500 dark:text-slate-400 font-medium">Selected Gateway</div>
            <div className="text-lg font-bold text-slate-900 dark:text-white mt-1">
              {activeVpnLocation.city} ({activeVpnLocation.country})
            </div>
            <div className="text-[11px] text-blue-600 dark:text-blue-400 mt-2 font-medium">Sample ping: {activeVpnLocation.ping}ms</div>
          </div>

          <div className="p-5 rounded-2xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-sm">
            <div className="text-xs text-slate-500 dark:text-slate-400 font-medium">Cipher Label</div>
            <div className="text-lg font-mono font-bold text-slate-900 dark:text-white mt-1">ChaCha20-Poly1305</div>
            <div className="text-[11px] text-slate-500 dark:text-slate-400 mt-2 font-medium">Display text only · no cipher is negotiated</div>
          </div>

          <div className="p-5 rounded-2xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-sm">
            <div className="text-xs text-slate-500 dark:text-slate-400 font-medium">Kill Switch</div>
            <div className="text-lg font-bold text-slate-900 dark:text-white mt-1">
              {vpnEnabled ? 'Simulated: armed' : 'Simulated: off'}
            </div>
            <div className="text-[11px] text-slate-500 dark:text-slate-400 mt-2 font-medium">No traffic is blocked or dropped</div>
          </div>
        </div>

        {/* Region Selector */}
        <div className="space-y-4">
          <h3 className="text-sm font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300">
            Select Gateway (sample inventory)
          </h3>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {vpnLocations.map(loc => {
              const isSelected = activeVpnLocation.id === loc.id;
              return (
                <div
                  key={loc.id}
                  onClick={() => {
                    setActiveVpnLocation(loc);
                    setVpnEnabled(true);
                  }}
                  className={`p-5 rounded-2xl border cursor-pointer transition-all flex items-center justify-between ${
                    isSelected && vpnEnabled
                      ? 'border-brand-500 bg-brand-50/50 dark:bg-brand-950/40 shadow-md'
                      : 'border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 hover:border-slate-300'
                  }`}
                >
                  <div className="flex items-center gap-3">
                    <span className="text-3xl">{loc.flag}</span>
                    <div>
                      <h4 className="text-sm font-bold text-slate-900 dark:text-white">
                        {loc.city}, {loc.country}
                      </h4>
                      <p className="text-xs text-slate-500 dark:text-slate-400 font-mono mt-0.5">
                        {loc.ip} · load {loc.load} (sample)
                      </p>
                    </div>
                  </div>

                  <div className="text-right">
                    <div className="text-xs font-mono font-semibold text-emerald-600 dark:text-emerald-400">
                      {loc.ping}ms
                    </div>
                    {isSelected && vpnEnabled && (
                      <span className="text-[10px] font-bold text-brand-600 dark:text-brand-400 uppercase tracking-wider">
                        Selected
                      </span>
                    )}
                  </div>
                </div>
              );
            })}
          </div>

          <div className="flex items-center gap-2 text-[11px] text-slate-500 dark:text-slate-400">
            {vpnEnabled
              ? <ShieldCheck className="w-3.5 h-3.5 text-emerald-500" />
              : <ShieldAlert className="w-3.5 h-3.5 text-slate-400" />}
            <span>
              The {vpnEnabled ? 'ON' : 'OFF'} state is only a UI flag shared with the browser and navbar badges; selecting a gateway changes nothing on the network.
            </span>
          </div>
        </div>
      </div>
    </div>
  );
};
