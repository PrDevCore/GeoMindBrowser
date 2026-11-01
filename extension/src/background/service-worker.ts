import { installGeolocationBlocks } from "./geolocation.js";
import { installHeaderRules } from "./headers.js";
import { NativeClient, ProtocolError } from "./native-client.js";
import { installPac, pacSatisfied, readPacSetting, RoutingError, type ValidatedPac } from "./pac.js";
import { applyWebRtcPolicy, watchWebRtcPolicy } from "./privacy.js";
import { badgeFor, parseTunnelStatus, type Badge, type RoutingState, type TunnelStatus } from "./status.js";
import {
  GEOLOCATION_KEY,
  HEADERS_ENABLED_KEY,
  PAC_KEY,
  POLICY_KEY,
  PRIVACY_KEY,
  STATUS_KEY,
} from "../lib/keys.js";

const HOST_NAME = "com.geomind.companion";
const PROTOCOL_VERSION = 1;
const TUNNEL_POLL_MS = 5_000;
const PAC_CHECK_MS = 5_000;
const DRIFT_DETAIL = "proxy settings changed";

type ConnectionState = "connected" | "disconnected" | "error";

interface CompanionStatus {
  readonly state: ConnectionState;
  readonly at: string;
  readonly protocolVersion?: number;
  readonly policyVersion?: number;
  readonly policyLoaded?: boolean;
  readonly tunnel?: TunnelStatus;
  readonly routing?: RoutingState;
  readonly routingDetail?: string;
  readonly ruleCount?: number;
  readonly code?: string;
}

interface Hello {
  readonly protocolVersion: number;
  readonly policyVersion: number;
  readonly tunnel: TunnelStatus;
}

let client: NativeClient | null = null;
let lastStatus: CompanionStatus | null = null;
let pollTimer: ReturnType<typeof setInterval> | null = null;
let pollInFlight = false;
let policyInFlight = false;
let policyDirty = false;
let headersEnabled = false;
let policyDocument: unknown = null;
let appliedHeaderDoc: unknown = null;
let appliedGeolocationDoc: unknown = null;
let appliedHeaderLocale: string | null | undefined;
let appliedHeaderEnabled: boolean | null = null;
let persistChain: Promise<void> = Promise.resolve();
let installedPac: string | null = null;
let pacGeneration = 0;
let pacWrites: Promise<unknown> = Promise.resolve();
let pacWritesPending = 0;
let pacGuardTimer: ReturnType<typeof setInterval> | null = null;
let pacGuardInFlight = false;
let proxyDrift = false;
let preDriftStatus: CompanionStatus | null = null;
let privacyState: "active" | "error" | undefined;

function timestamp(): string {
  return new Date().toISOString();
}

/**
 * Status is written in call order: lastStatus is updated synchronously and each
 * storage write is chained. The tunnel poll and a policy refresh can overlap
 * without an older snapshot landing last.
 */
function writeStatus(status: CompanionStatus): Promise<void> {
  lastStatus = status;
  const write = persistChain
    .then(() => chrome.storage.session.set({ [STATUS_KEY]: status }))
    .catch(() => undefined);
  persistChain = write;
  return write;
}

/** The badge is the only routing UI in this step; it must never be silent. */
function applyBadge(badge: Badge): void {
  try {
    void chrome.action.setBadgeText({ text: badge.text });
    void chrome.action.setBadgeBackgroundColor({ color: badge.color });
    void chrome.action.setTitle({ title: badge.title });
  } catch {
    // Badge updates are best effort; routing state is also stored.
  }
}

/**
 * Every PAC write goes through one queue so a drift repair and a policy refresh
 * cannot land out of order. The generation records the newest requested PAC so
 * a repair never resurrects a policy that has already been replaced.
 */
function applyPac(pac: string): Promise<ValidatedPac> {
  const generation = ++pacGeneration;
  pacWritesPending += 1;
  const run = pacWrites.then(() => installPac(pac)).then((applied) => {
    if (generation === pacGeneration) {
      installedPac = pac;
      void chrome.storage.session.set({ [PAC_KEY]: pac }).catch(() => undefined);
      startPacGuard();
    }
    return applied;
  });
  const settled = run.then(
    () => {
      pacWritesPending -= 1;
    },
    () => {
      pacWritesPending -= 1;
    },
  );
  pacWrites = settled;
  return run;
}

function startPacGuard(): void {
  if (pacGuardTimer !== null) {
    return;
  }
  pacGuardTimer = setInterval(() => {
    void guardPac();
  }, PAC_CHECK_MS);
}

function stopPacGuard(): void {
  if (pacGuardTimer !== null) {
    clearInterval(pacGuardTimer);
    pacGuardTimer = null;
  }
}

/**
 * Another extension or an enterprise policy can take the proxy setting away at
 * any time. Re-assert the last valid PAC; if that fails, stop claiming routing
 * is active. The stale PAC fails closed, but a foreign setting may not.
 */
async function guardPac(): Promise<void> {
  const expected = installedPac;
  if (expected === null || pacGuardInFlight || pacWritesPending > 0) {
    return;
  }
  pacGuardInFlight = true;
  const generation = pacGeneration;
  try {
    let snapshot;
    try {
      snapshot = await readPacSetting();
    } catch {
      return;
    }
    if (pacSatisfied(snapshot, expected)) {
      await clearProxyDrift();
      return;
    }
    if (installedPac !== expected || pacGeneration !== generation || pacWritesPending > 0) {
      return;
    }
    try {
      await applyPac(expected);
    } catch {
      await markProxyDrift();
      return;
    }
    const repaired = await readPacSetting().catch(() => null);
    if (repaired === null || !pacSatisfied(repaired, expected)) {
      await markProxyDrift();
    } else {
      await clearProxyDrift();
    }
  } finally {
    pacGuardInFlight = false;
  }
}

async function markProxyDrift(): Promise<void> {
  if (!proxyDrift) {
    preDriftStatus = lastStatus;
  }
  proxyDrift = true;
  const base: CompanionStatus =
    lastStatus ?? { state: "error", at: timestamp(), code: "proxy_settings_changed" };
  const status: CompanionStatus = {
    ...base,
    at: timestamp(),
    routing: "error",
    routingDetail: DRIFT_DETAIL,
  };
  await writeStatus(status);
  applyBadge(badgeFor("error", DRIFT_DETAIL, status.tunnel?.state, privacyState));
}

async function clearProxyDrift(): Promise<void> {
  if (!proxyDrift) {
    return;
  }
  proxyDrift = false;
  const restore = preDriftStatus;
  preDriftStatus = null;
  const current = lastStatus;
  if (restore === null || current === null || current.routingDetail !== DRIFT_DETAIL) {
    // A newer status (for example a disconnect) is already the truthful one.
    return;
  }
  await writeStatus({ ...restore, at: timestamp() });
  applyBadge(badgeFor(restore.routing ?? "unavailable", restore.routingDetail, restore.tunnel?.state, privacyState));
}

/** Restores the guard after a service-worker restart within the same session. */
async function restorePacGuard(): Promise<void> {
  try {
    const stored = await chrome.storage.session.get(PAC_KEY);
    const pac = stored[PAC_KEY];
    if (typeof pac === "string" && pac.length > 0) {
      installedPac = pac;
      startPacGuard();
      void guardPac();
    }
  } catch {
    // Without a stored PAC the companion sync installs a fresh one.
  }
}

async function syncPrivacyPolicy(): Promise<void> {
  try {
    await applyWebRtcPolicy();
    privacyState = "active";
    await chrome.storage.session.set({ [PRIVACY_KEY]: { state: "active", at: timestamp() } });
  } catch {
    privacyState = "error";
    await chrome.storage.session.set({ [PRIVACY_KEY]: { state: "error", at: timestamp() } }).catch(() => undefined);
  }
  if (lastStatus !== null) {
    applyBadge(
      badgeFor(
        lastStatus.routing ?? "unavailable",
        lastStatus.routingDetail,
        lastStatus.tunnel?.state,
        privacyState,
      ),
    );
  }
}

watchWebRtcPolicy(() => {
  void syncPrivacyPolicy();
});

function parseHello(payload: Record<string, unknown> | undefined): Hello {
  if (payload === undefined) {
    throw new ProtocolError("malformed", "hello result has no payload");
  }
  const protocolVersion = payload.protocolVersion;
  const policyVersion = payload.policyVersion;
  const tunnel = payload.tunnel;
  if (typeof protocolVersion !== "number" || !Number.isInteger(protocolVersion)) {
    throw new ProtocolError("malformed", "hello result has no protocol version");
  }
  if (typeof policyVersion !== "number" || !Number.isInteger(policyVersion)) {
    throw new ProtocolError("malformed", "hello result has no policy version");
  }
  if (typeof tunnel !== "object" || tunnel === null || Array.isArray(tunnel)) {
    throw new ProtocolError("malformed", "hello result has no tunnel status");
  }
  return {
    protocolVersion,
    policyVersion,
    tunnel: parseTunnelStatus(tunnel as Record<string, unknown>),
  };
}

function parsePolicyVersion(policy: unknown): number {
  if (typeof policy !== "object" || policy === null || Array.isArray(policy)) {
    throw new ProtocolError("malformed", "policy result has no policy");
  }
  const version = (policy as Record<string, unknown>).version;
  if (typeof version !== "number" || !Number.isInteger(version) || version < 0) {
    throw new ProtocolError("malformed", "policy version is invalid");
  }
  return version;
}

interface PolicySync {
  readonly policyVersion: number;
  readonly routing: RoutingState;
  readonly routingDetail?: string;
  readonly ruleCount?: number;
  /** Whether this call installed a PAC. A failed call leaves the old one in place. */
  readonly installed: boolean;
}

/**
 * Fetches the policy, validates the PAC, and installs it. The PAC swap is the
 * routing switch: on any failure the previously installed PAC stays active and
 * still fails closed, so a bad reload never opens a direct path.
 */
async function syncPolicy(active: NativeClient, fallbackVersion: number): Promise<PolicySync> {
  try {
    const envelope = await active.request("policy.get", "policy.result", {});
    const payload = envelope.payload;
    if (payload === undefined) {
      throw new ProtocolError("malformed", "policy result has no payload");
    }
    const policyVersion = parsePolicyVersion(payload.policy);
    const pac = payload.pac;
    if (typeof pac !== "string" || pac.length === 0) {
      throw new ProtocolError("malformed", "policy result has no PAC");
    }
    const applied = await applyPac(pac);
    policyDocument = payload.policy;
    void persistPolicyDocument();
    return {
      policyVersion,
      routing: "active",
      ruleCount: applied.routeCount + applied.blockCount,
      installed: true,
    };
  } catch (error) {
    if (error instanceof ProtocolError && error.code === "policy_unavailable") {
      return {
        policyVersion: fallbackVersion,
        routing: "unavailable",
        routingDetail: "policy unavailable",
        installed: false,
      };
    }
    return {
      policyVersion: fallbackVersion,
      routing: "error",
      routingDetail: error instanceof RoutingError ? error.code : "invalid policy",
      installed: false,
    };
  }
}

function stopTunnelPolling(): void {
  if (pollTimer !== null) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

async function persistPolicyDocument(): Promise<void> {
  try {
    await chrome.storage.session.set({ [POLICY_KEY]: policyDocument });
  } catch {
    // The options page may show no sites; routing itself is unaffected.
  }
}

async function loadHeadersEnabled(): Promise<void> {
  try {
    const stored = await chrome.storage.local.get(HEADERS_ENABLED_KEY);
    headersEnabled = stored[HEADERS_ENABLED_KEY] === true;
  } catch {
    headersEnabled = false;
  }
}

/**
 * Applies locale header rules for the current policy, tunnel locale, and user
 * choice. Policy edits and locale changes re-plan the rules; nothing else is
 * modified. Failures keep the previous rules until the next status update.
 */
async function syncHeaderRulesFromState(): Promise<void> {
  if (policyDocument === null) {
    return;
  }
  const document = policyDocument;
  const locale = lastStatus?.tunnel?.locale ?? null;
  const enabled = headersEnabled;
  if (document === appliedHeaderDoc && locale === appliedHeaderLocale && enabled === appliedHeaderEnabled) {
    return;
  }
  try {
    await installHeaderRules(document, locale, enabled);
    // Record only the inputs that were actually installed; a change during the
    // await leaves the markers stale so the next status update retries.
    if (
      policyDocument === document &&
      (lastStatus?.tunnel?.locale ?? null) === locale &&
      headersEnabled === enabled
    ) {
      appliedHeaderDoc = document;
      appliedHeaderLocale = locale;
      appliedHeaderEnabled = enabled;
    }
  } catch {
    // Keep the installed rules; the next status update retries.
  }
}

/**
 * Blocks geolocation for routed origins whenever the policy document changes.
 * Blocks are monotonic, so a failed apply leaves the earlier ones in place and
 * the next policy update retries.
 */
async function syncGeolocationFromState(): Promise<void> {
  if (policyDocument === null || policyDocument === appliedGeolocationDoc) {
    return;
  }
  const document = policyDocument;
  try {
    const count = await installGeolocationBlocks(document);
    if (policyDocument === document) {
      appliedGeolocationDoc = document;
    }
    await chrome.storage.session.set({
      [GEOLOCATION_KEY]: { state: "blocked", count, at: timestamp() },
    });
  } catch {
    await chrome.storage.session
      .set({ [GEOLOCATION_KEY]: { state: "error", at: timestamp() } })
      .catch(() => undefined);
  }
}

function startTunnelPolling(active: NativeClient): void {
  stopTunnelPolling();
  pollTimer = setInterval(() => {
    void refreshTunnelStatus(active);
  }, TUNNEL_POLL_MS);
}

/**
 * Refreshes tunnel state without touching the installed PAC. A failed poll keeps
 * the last known state; only a disconnect proves the companion is gone, and
 * routed traffic fails closed either way.
 */
async function refreshTunnelStatus(active: NativeClient): Promise<void> {
  if (pollInFlight || client !== active || lastStatus === null || lastStatus.state !== "connected") {
    return;
  }
  pollInFlight = true;
  try {
    const envelope = await active.request("tunnel.status", "tunnel.status.result", {});
    const tunnel = parseTunnelStatus(envelope.payload);
    const status: CompanionStatus = { ...lastStatus, at: timestamp(), tunnel };
    await writeStatus(status);
    applyBadge(badgeFor(status.routing ?? "unavailable", status.routingDetail, tunnel.state, privacyState));
    void syncHeaderRulesFromState();
  } catch (error) {
    if (error instanceof ProtocolError && error.code === "disconnected") {
      stopTunnelPolling();
    }
  } finally {
    pollInFlight = false;
  }
}

/**
 * Reinstalls the PAC after the companion reports a policy change. A failed
 * refresh is reported in the status but never tears down the connection: the
 * previous PAC stays installed and routed traffic keeps failing closed.
 */
async function refreshPolicy(active: NativeClient): Promise<void> {
  if (client !== active) {
    return;
  }
  if (policyInFlight) {
    policyDirty = true;
    return;
  }
  const started = lastStatus;
  if (started === null || started.state !== "connected") {
    return;
  }
  policyInFlight = true;
  try {
    const policySync = await syncPolicy(active, started.policyVersion ?? 0);
    // The tunnel poll may have written while the policy request was in flight.
    const latest = lastStatus;
    if (client !== active || latest === null || latest.state !== "connected") {
      return;
    }
    const ruleCount = policySync.installed ? policySync.ruleCount : latest.ruleCount;
    const status: CompanionStatus = {
      state: "connected",
      at: timestamp(),
      policyVersion: policySync.policyVersion,
      policyLoaded: policySync.installed || (latest.policyLoaded ?? false),
      routing: policySync.routing,
      ...(latest.protocolVersion === undefined ? {} : { protocolVersion: latest.protocolVersion }),
      ...(latest.tunnel === undefined ? {} : { tunnel: latest.tunnel }),
      ...(policySync.routingDetail === undefined ? {} : { routingDetail: policySync.routingDetail }),
      ...(ruleCount === undefined ? {} : { ruleCount }),
    };
    await writeStatus(status);
    applyBadge(badgeFor(status.routing ?? "unavailable", status.routingDetail, status.tunnel?.state, privacyState));
    void syncHeaderRulesFromState();
    void syncGeolocationFromState();
  } finally {
    policyInFlight = false;
    if (policyDirty) {
      policyDirty = false;
      void refreshPolicy(active);
    }
  }
}

async function connectAndSync(): Promise<void> {
  if (client !== null) {
    return;
  }
  let port: chrome.runtime.Port;
  try {
    port = chrome.runtime.connectNative(HOST_NAME);
  } catch {
    // A missing host manifest fails closed: no tunnel, no routing.
    await writeStatus({ state: "error", at: timestamp(), code: "connect_failed" });
    applyBadge(badgeFor("error", "companion unavailable", undefined, privacyState));
    return;
  }

  const active = new NativeClient(port, {});
  client = active;
  active.onDisconnect = () => {
    if (client === active) {
      client = null;
      stopTunnelPolling();
      policyDirty = false;
      void writeStatus({ state: "disconnected", at: timestamp(), code: "disconnected" });
      // The last valid PAC stays installed. Routed hosts keep failing closed
      // through the companion proxy instead of falling back to the real IP.
      applyBadge(badgeFor("error", "companion disconnected", undefined, privacyState));
    }
  };
  active.onProtocolError = (error) => {
    void writeStatus({
      state: "error",
      at: timestamp(),
      code: error instanceof ProtocolError ? error.code : "malformed",
    });
  };
  active.onEvent = (envelope) => {
    if (envelope.type === "policy.changed") {
      void refreshPolicy(active);
    }
  };

  try {
    const helloEnvelope = await active.request("hello", "hello.result", {
      extensionVersion: chrome.runtime.getManifest().version,
    });
    const hello = parseHello(helloEnvelope.payload);
    if (hello.protocolVersion !== PROTOCOL_VERSION) {
      stopTunnelPolling();
      policyDirty = false;
      await writeStatus({ state: "error", at: timestamp(), code: "protocol_mismatch" });
      applyBadge(badgeFor("error", "protocol mismatch", undefined, privacyState));
      active.disconnect();
      if (client === active) {
        client = null;
      }
      return;
    }

    await writeStatus({
      state: "connected",
      at: timestamp(),
      protocolVersion: hello.protocolVersion,
      policyVersion: hello.policyVersion,
      tunnel: hello.tunnel,
    });

    policyInFlight = true;
    const policySync = await syncPolicy(active, hello.policyVersion).finally(() => {
      policyInFlight = false;
    });
    const statusEnvelope = await active.request("tunnel.status", "tunnel.status.result", {});
    const tunnel = parseTunnelStatus(statusEnvelope.payload);
    await writeStatus({
      state: "connected",
      at: timestamp(),
      protocolVersion: hello.protocolVersion,
      policyVersion: policySync.policyVersion,
      policyLoaded: policySync.installed,
      routing: policySync.routing,
      tunnel,
      ...(policySync.routingDetail === undefined ? {} : { routingDetail: policySync.routingDetail }),
      ...(policySync.ruleCount === undefined ? {} : { ruleCount: policySync.ruleCount }),
    });
    applyBadge(badgeFor(policySync.routing, policySync.routingDetail, tunnel.state, privacyState));
    startTunnelPolling(active);
    void syncHeaderRulesFromState();
    void syncGeolocationFromState();
    if (policyDirty) {
      policyDirty = false;
      void refreshPolicy(active);
    }
  } catch (error) {
    stopTunnelPolling();
    policyDirty = false;
    await writeStatus({
      state: "error",
      at: timestamp(),
      code: error instanceof ProtocolError ? error.code : "error",
    });
    applyBadge(badgeFor("error", error instanceof ProtocolError ? error.code : "error", undefined, privacyState));
    active.disconnect();
    if (client === active) {
      client = null;
    }
  }
}

chrome.runtime.onInstalled.addListener(() => {
  void syncPrivacyPolicy();
  void connectAndSync();
});

chrome.runtime.onStartup.addListener(() => {
  void syncPrivacyPolicy();
  void connectAndSync();
});

chrome.runtime.onSuspend.addListener(() => {
  stopTunnelPolling();
  stopPacGuard();
  policyDirty = false;
  client?.disconnect();
  client = null;
});

chrome.proxy.settings.onChange.addListener(() => {
  void guardPac();
});

chrome.storage.onChanged.addListener((changes, area) => {
  if (area !== "local" || !(HEADERS_ENABLED_KEY in changes)) {
    return;
  }
  headersEnabled = changes[HEADERS_ENABLED_KEY]?.newValue === true;
  appliedHeaderEnabled = null;
  void syncHeaderRulesFromState();
});

void loadHeadersEnabled().then(() => {
  appliedHeaderEnabled = null;
  void syncHeaderRulesFromState();
});

void syncPrivacyPolicy();
void restorePacGuard();
void connectAndSync();
