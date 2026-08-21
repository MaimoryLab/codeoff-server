import {AppService} from "../bindings/github.com/MaimoryLab/codex-server";
import type {Snapshot, ToolStatus} from "../bindings/github.com/MaimoryLab/codex-server/internal/diagnostics/models.js";
import {Clipboard} from "@wailsio/runtime";

const checkedAt = document.querySelector<HTMLElement>("#checked-at")!;
const platform = document.querySelector<HTMLElement>("#platform")!;
const message = document.querySelector<HTMLElement>("#message")!;
const refreshButton = document.querySelector<HTMLButtonElement>("#refresh")!;
const installNodeButton = document.querySelector<HTMLButtonElement>("#install-node")!;
const installCodexButton = document.querySelector<HTMLButtonElement>("#install-codex")!;
const installCloudflaredButton = document.querySelector<HTMLButtonElement>("#install-cloudflared")!;
const appServerState = document.querySelector<HTMLElement>("#app-server-state")!;
const appServerDetail = document.querySelector<HTMLElement>("#app-server-detail")!;
const toggleAppServerButton = document.querySelector<HTMLButtonElement>("#toggle-app-server")!;
const tunnelState = document.querySelector<HTMLElement>("#tunnel-state")!;
const tunnelDetail = document.querySelector<HTMLElement>("#tunnel-detail")!;
const toggleTunnelButton = document.querySelector<HTMLButtonElement>("#toggle-tunnel")!;
const bindDeviceButton = document.querySelector<HTMLButtonElement>("#bind-device")!;
const pairingCode = document.querySelector<HTMLElement>("#pairing-code")!;
const pairingValue = document.querySelector<HTMLElement>("#pairing-value")!;
const copyPairingButton = document.querySelector<HTMLButtonElement>("#copy-pairing")!;
const deviceList = document.querySelector<HTMLUListElement>("#device-list")!;
let appServerRunning = false;
let tunnelRunning = false;
let tunnelURL = "";
let pairingToken = "";
let pairingExpiresAt = "";
let controlAddr = "";

type RuntimeState = {
    running: boolean;
    starting: boolean;
    startedAt: string;
    userAgent?: string;
    error?: string;
};

type TunnelState = { running: boolean; starting: boolean; url?: string; error?: string };

type Device = { id: string; name: string; createdAt: string; lastSeen: string };

const tools: Record<string, HTMLElement> = {
    node: document.querySelector<HTMLElement>("#tool-node")!,
    codex: document.querySelector<HTMLElement>("#tool-codex")!,
    appServer: document.querySelector<HTMLElement>("#tool-app-server")!,
    cloudflared: document.querySelector<HTMLElement>("#tool-cloudflared")!,
};

function renderTool(element: HTMLElement, tool: ToolStatus) {
    element.className = `tool-status ${tool.installed ? "ready" : "missing"}`;
    element.textContent = tool.installed ? tool.version || "Installed" : tool.error || "Not installed";
}

function render(snapshot: Snapshot) {
    platform.textContent = `${snapshot.platform} / ${snapshot.architecture}`;
    checkedAt.textContent = `Checked ${new Date(snapshot.checkedAt).toLocaleTimeString()}`;
    renderTool(tools.node, snapshot.node);
    renderTool(tools.codex, snapshot.codex);
    renderTool(tools.appServer, snapshot.appServer);
    renderTool(tools.cloudflared, snapshot.cloudflared);
    installNodeButton.disabled = snapshot.node.installed;
    installNodeButton.textContent = snapshot.node.installed ? "Installed" : "Install";
    installCodexButton.disabled = snapshot.codex.installed;
    installCodexButton.textContent = snapshot.codex.installed ? "Installed" : "Install";
    installCloudflaredButton.disabled = snapshot.cloudflared.installed;
    installCloudflaredButton.textContent = snapshot.cloudflared.installed ? "Installed" : "Install";
    message.textContent = "Environment check complete";
}

function renderAppServer(state: RuntimeState, address = controlAddr) {
    appServerRunning = state.running;
    appServerState.textContent = state.starting ? "Starting" : state.running ? "Running" : "Offline";
    appServerState.className = state.running ? "state-online" : "state-offline";
    appServerDetail.textContent = address || state.error || "Stopped";
    toggleAppServerButton.textContent = state.running ? "Stop" : "Start";
    toggleAppServerButton.disabled = state.starting;
}

function renderTunnel(state: TunnelState) {
    tunnelRunning = state.running;
    tunnelURL = state.url || "";
    tunnelState.textContent = state.starting ? "Starting" : state.running ? "Online" : "Offline";
    tunnelState.className = state.running ? "state-online" : "state-offline";
    tunnelDetail.textContent = state.error || state.url || "Stopped";
    toggleTunnelButton.textContent = state.running ? "Stop" : "Start";
    toggleTunnelButton.disabled = state.starting;
    updatePairingCode();
}

function updatePairingCode() {
    if (!pairingToken) {
        return;
    }
    const endpoint = tunnelURL ? ` · endpoint ${tunnelURL}/api/v1/pair/exchange` : "";
    pairingValue.textContent = `Pairing code: ${pairingToken}${endpoint} · expires ${pairingExpiresAt}`;
}

function hidePairingCode() {
    pairingToken = "";
    pairingCode.hidden = true;
}

async function refresh() {
    refreshButton.disabled = true;
    message.textContent = "Checking local environment...";
    try {
        const [environment, overview] = await Promise.all([AppService.RefreshStatus(), AppService.Overview()]);
        controlAddr = overview.controlAddr || "";
        render(environment);
        renderAppServer(overview.appServer);
        renderTunnel(overview.tunnel);
        await refreshDevices();
    } catch (error) {
        message.textContent = error instanceof Error ? error.message : "Unable to check environment";
    } finally {
        refreshButton.disabled = false;
    }
}

async function toggleTunnel() {
    toggleTunnelButton.disabled = true;
    message.textContent = tunnelRunning ? "Stopping tunnel..." : "Starting tunnel...";
    try {
        const state = tunnelRunning ? await AppService.StopTunnel() : await AppService.StartTunnel();
        renderTunnel(state);
        message.textContent = state.running ? "Tunnel is online" : "Tunnel stopped";
    } catch (error) {
        message.textContent = error instanceof Error ? error.message : "Unable to change tunnel state";
        renderTunnel(await AppService.TunnelState());
    }
}

async function toggleAppServer() {
    toggleAppServerButton.disabled = true;
    message.textContent = appServerRunning ? "Stopping app-server..." : "Starting app-server...";
    try {
        const state = appServerRunning ? await AppService.StopAppServer() : await AppService.StartAppServer();
        renderAppServer(state);
        message.textContent = state.running ? "App-server is running" : "App-server stopped";
    } catch (error) {
        message.textContent = error instanceof Error ? error.message : "Unable to change app-server state";
        renderAppServer(await AppService.AppServerState());
    }
}

function renderDevices(devices: Device[]) {
    deviceList.replaceChildren();
    if (devices.length === 0) {
        const empty = document.createElement("li");
        empty.className = "muted";
        empty.textContent = "No devices bound";
        deviceList.append(empty);
        return;
    }
    for (const device of devices) {
        const item = document.createElement("li");
        const label = document.createElement("span");
        label.textContent = `${device.name} · ${new Date(device.lastSeen).toLocaleString()}`;
        const revoke = document.createElement("button");
        revoke.className = "mini-button";
        revoke.textContent = "Revoke";
        revoke.addEventListener("click", () => void revokeDevice(device.id));
        item.append(label, revoke);
        deviceList.append(item);
    }
}

async function refreshDevices() {
    try {
        const [devices, pairingActive] = await Promise.all([AppService.Devices(), AppService.PairingActive()]);
        renderDevices(devices ?? []);
        if (pairingToken && !pairingActive) hidePairingCode();
    } catch (error) {
        message.textContent = error instanceof Error ? error.message : "Unable to load devices";
    }
}

async function bindDevice() {
    bindDeviceButton.disabled = true;
    try {
        const pairing = await AppService.NewPairing();
        pairingToken = pairing.token;
        pairingExpiresAt = new Date(pairing.expiresAt).toLocaleTimeString();
        pairingCode.hidden = false;
        updatePairingCode();
    } catch (error) {
        message.textContent = error instanceof Error ? error.message : "Unable to create pairing code";
    } finally {
        bindDeviceButton.disabled = false;
    }
}

async function copyPairingCode() {
    if (!pairingToken) return;
    try {
        await Clipboard.SetText(pairingToken);
        message.textContent = "Pairing code copied";
    } catch (error) {
        message.textContent = error instanceof Error ? error.message : "Unable to copy pairing code";
    }
}

async function revokeDevice(id: string) {
    try {
        await AppService.RevokeDevice(id);
        await refreshDevices();
    } catch (error) {
        message.textContent = error instanceof Error ? error.message : "Unable to revoke device";
    }
}

async function install(kind: "node" | "codex" | "cloudflared") {
    const button = kind === "node" ? installNodeButton : kind === "codex" ? installCodexButton : installCloudflaredButton;
    button.disabled = true;
    const label = kind === "node" ? "Node.js" : kind === "codex" ? "Codex CLI" : "Cloudflared";
    message.textContent = `Installing ${label}...`;
    try {
        const result = kind === "node" ? await AppService.InstallNode() : kind === "codex" ? await AppService.InstallCodex() : await AppService.InstallCloudflared();
        render(result);
    } catch (error) {
        message.textContent = error instanceof Error ? error.message : "Installation failed";
        button.disabled = false;
    }
}

refreshButton.addEventListener("click", refresh);
installNodeButton.addEventListener("click", () => void install("node"));
installCodexButton.addEventListener("click", () => void install("codex"));
installCloudflaredButton.addEventListener("click", () => void install("cloudflared"));
toggleAppServerButton.addEventListener("click", () => void toggleAppServer());
toggleTunnelButton.addEventListener("click", () => void toggleTunnel());
bindDeviceButton.addEventListener("click", () => void bindDevice());
copyPairingButton.addEventListener("click", () => void copyPairingCode());
void refresh();
void refreshDevices();
window.setInterval(() => {
    void Promise.all([AppService.AppServerState(), AppService.TunnelState()]).then(([runtime, tunnel]) => { renderAppServer(runtime); renderTunnel(tunnel); }).catch(() => undefined);
    void refreshDevices();
}, 5000);
