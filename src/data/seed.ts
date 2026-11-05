import { BrowserTab, ChatMessage, DocItem, TaskItem, TeamMember, VpnLocation } from '../types';

// Illustrative demo content only. VPN addresses, pings and loads are not measured
// from real gateways; the app establishes no tunnel. The server copies these seeds
// into data/store.json on first read, after which the stored values are the source of truth.

const minutesAgo = (minutes: number) => Date.now() - minutes * 60_000;

export const VPN_LOCATIONS: VpnLocation[] = [
  { id: '1', country: 'United Kingdom', city: 'London (UK Gateway)', flag: '🇬🇧', ip: '193.122.64.12', ping: 18, load: '28%' },
  { id: '2', country: 'Germany', city: 'Frankfurt (EU Core)', flag: '🇩🇪', ip: '185.220.101.45', ping: 14, load: '32%' },
  { id: '3', country: 'France', city: 'Paris (EU West)', flag: '🇫🇷', ip: '195.154.122.8', ping: 21, load: '36%' },
  { id: '4', country: 'Switzerland', city: 'Zurich (Secure Zone)', flag: '🇨🇭', ip: '194.126.180.5', ping: 16, load: '24%' },
  { id: '5', country: 'Japan', city: 'Tokyo (APAC)', flag: '🇯🇵', ip: '203.0.113.195', ping: 48, load: '45%' },
  { id: '6', country: 'United States', city: 'New York (US-East)', flag: '🇺🇸', ip: '198.51.100.22', ping: 22, load: '58%' },
];

export const CURRENT_USER = {
  id: 'local-user',
  name: 'You',
  role: 'Remote Lead',
  displayName: 'You (Remote Lead)',
};

export const SEED_TEAM: TeamMember[] = [
  { id: '1', name: 'Sarah Chen', role: 'Lead Architect', status: 'online', currentActivity: 'Reviewing gateway config', vpnLocation: 'London (UK)' },
  { id: '2', name: 'Alex Rivera', role: 'SecOps', status: 'online', currentActivity: 'Auditing firewall rules', vpnLocation: 'Frankfurt (EU)' },
  { id: '3', name: 'Elena Rostova', role: 'Product', status: 'busy', currentActivity: 'Writing the offsite playbook', vpnLocation: 'Zurich (Secure Zone)' },
  { id: '4', name: 'Marcus Vance', role: 'Dev', status: 'away', currentActivity: 'Offline until 14:00', vpnLocation: 'New York (US-East)' },
];

export const SEED_TASKS: TaskItem[] = [
  {
    id: '1',
    title: 'Deploy WireGuard VPN Gateway',
    description: 'Set up encrypted EU/UK tunneling endpoints.',
    status: 'in_progress',
    priority: 'urgent',
    assignee: { name: 'Sarah Chen', role: 'Lead Architect' },
    dueDate: '2026-11-12',
    vpnRequired: true,
    tags: ['Security'],
  },
  {
    id: '2',
    title: 'Sync real-time team doc specs',
    description: 'Verify WebSocket real-time collaboration.',
    status: 'done',
    priority: 'medium',
    assignee: { name: 'Alex Rivera', role: 'SecOps' },
    dueDate: '2026-11-14',
    vpnRequired: false,
    tags: ['Sync'],
  },
];

export const SEED_DOCS: DocItem[] = [
  {
    id: '1',
    title: 'GeoMind Demo Scope & Simulation Notes',
    content: `# GeoMind Demo Scope & Simulation Notes

## 1. What this build actually does
Persists tasks, documents and chat messages to a JSON store on the local server.

## 2. What is simulated
- The VPN Suite is a UI mock-up. No tunnel is created, no traffic is routed or encrypted.
- Gateway IPs, ping and load figures are illustrative sample values.
- Team presence and the browser tab/bookmark presets are sample content.

## 3. Consequences for the UI
- Security badges that imply transport protection have been removed.
- Anything labelled "Simulated" or "Sample" must not be relied on for real operations.`,
    lastEditedBy: 'Sarah Chen',
    updatedAt: minutesAgo(10),
    category: 'Architecture',
  },
  {
    id: '2',
    title: 'Global Offsite & Team Bonding Playbook',
    content: `# Global Offsite Playbook

## Destination: Zurich, Switzerland
- Dates: November 18 - November 22, 2026.
- Objectives: Q1 Roadmap finalization, team workshops, and security drills.
- Network: use the venue guest Wi-Fi with your normal corporate VPN client;
  the VPN panel inside GeoMind is a demo mock-up and does not protect traffic.`,
    lastEditedBy: 'Elena Rostova',
    updatedAt: minutesAgo(120),
    category: 'Operations',
  },
];

export const SEED_CHAT: ChatMessage[] = [
  {
    id: '1',
    senderName: 'Sarah Chen',
    text: 'Sample message: the VPN panel shows simulated gateway data, so treat anything under VPN Suite as a demo.',
    timestamp: minutesAgo(26),
    vpnNode: 'London 🇬🇧 (simulated)',
  },
];

export const SEARCH_ENGINES = [
  { name: 'DuckDuckGo', queryUrl: 'https://html.duckduckgo.com/html/?q=' },
  { name: 'Google', queryUrl: 'https://www.google.com/search?q=' },
  { name: 'Bing', queryUrl: 'https://www.bing.com/search?q=' },
  { name: 'Brave', queryUrl: 'https://search.brave.com/search?q=' },
];

// Only public, frameable sites: many hosts (GitHub, AWS, Figma) refuse to render in an iframe.
export const PRESET_SITES = [
  { title: 'Example.com', url: 'https://example.com', category: 'Sample', icon: '🌐' },
  { title: 'Wikipedia - Remote Work', url: 'https://en.wikipedia.org/wiki/Remote_work', category: 'External', icon: '📚' },
];

export const SEED_TABS: BrowserTab[] = [
  {
    id: '1',
    url: 'https://example.com',
    title: 'Example.com',
    isLoading: false,
    history: ['https://example.com'],
    historyIndex: 0,
  },
  {
    id: '2',
    url: 'https://en.wikipedia.org/wiki/Remote_work',
    title: 'Wikipedia - Remote Work',
    isLoading: false,
    history: ['https://en.wikipedia.org/wiki/Remote_work'],
    historyIndex: 0,
  },
];
