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
const dashboard = document.querySelector<HTMLElement>("#dashboard")!;
const appServerState = document.querySelector<HTMLElement>("#app-server-state")!;
const appServerDetail = document.querySelector<HTMLElement>("#app-server-detail")!;
const appServerAddress = document.querySelector<HTMLElement>("#app-server-address")!;
const copyAppServerButton = document.querySelector<HTMLButtonElement>("#copy-app-server")!;
const listenHost = document.querySelector<HTMLInputElement>("#listen-host")!;
const listenPort = document.querySelector<HTMLInputElement>("#listen-port")!;
const saveListenButton = document.querySelector<HTMLButtonElement>("#save-listen")!;
const toggleAppServerButton = document.querySelector<HTMLButtonElement>("#toggle-app-server")!;
const tunnelState = document.querySelector<HTMLElement>("#tunnel-state")!;
const tunnelDetail = document.querySelector<HTMLElement>("#tunnel-detail")!;
const tunnelAddress = document.querySelector<HTMLElement>("#tunnel-address")!;
const tunnelAddressValue = document.querySelector<HTMLElement>("#tunnel-address-value")!;
const copyTunnelButton = document.querySelector<HTMLButtonElement>("#copy-tunnel")!;
const toggleTunnelButton = document.querySelector<HTMLButtonElement>("#toggle-tunnel")!;
const bindDeviceButton = document.querySelector<HTMLButtonElement>("#bind-device")!;
const pairingCode = document.querySelector<HTMLElement>("#pairing-code")!;
const pairingValue = document.querySelector<HTMLElement>("#pairing-value")!;
const pairingExpiry = document.querySelector<HTMLElement>("#pairing-expiry")!;
const copyPairingButton = document.querySelector<HTMLButtonElement>("#copy-pairing")!;
const deviceList = document.querySelector<HTMLUListElement>("#device-list")!;
let appServerRunning = false;
let tunnelRunning = false;
let tunnelURL = "";
let cloudflaredInstalled = false;
let pairingToken = "";
let pairingExpiresAt = "";
let controlAddr = "";

type RuntimeState = {
    running: boolean;
    starting: boolean;
    stopping: boolean;
    startedAt: string;
    userAgent?: string;
    error?: string;
};

type TunnelState = { running: boolean; starting: boolean; stopping: boolean; url?: string; error?: string };

type Device = { id: string; name: string; createdAt: string; lastSeen: string; connected?: boolean };

const tools: Record<string, HTMLElement> = {
    node: document.querySelector<HTMLElement>("#tool-node")!,
    codex: document.querySelector<HTMLElement>("#tool-codex")!,
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
    renderTool(tools.cloudflared, snapshot.cloudflared);
    dashboard.classList.toggle("ready", snapshot.node.installed && snapshot.codex.installed);
    installNodeButton.disabled = false;
    installNodeButton.textContent = snapshot.node.installed ? "Upgrade" : "Install";
    installCodexButton.disabled = false;
    installCodexButton.textContent = snapshot.codex.installed ? "Upgrade" : "Install";
    installCloudflaredButton.disabled = false;
    installCloudflaredButton.textContent = snapshot.cloudflared.installed ? "Upgrade" : "Install";
    cloudflaredInstalled = snapshot.cloudflared.installed;
    message.textContent = "";
}

function renderAppServer(state: RuntimeState, address = controlAddr) {
    appServerRunning = state.running;
    appServerState.textContent = state.starting ? "Starting" : state.stopping ? "Stopping" : state.running ? "Running" : "Offline";
    appServerState.className = state.running ? "state-online" : "state-offline";
    appServerDetail.textContent = state.error || (state.running ? "Local control endpoint" : "Stopped");
    appServerAddress.textContent = address || "-";
    copyAppServerButton.disabled = !address;
    toggleAppServerButton.textContent = state.running || state.starting ? "Stop" : "Start";
    toggleAppServerButton.disabled = state.starting || state.stopping;
}

function renderTunnel(state: TunnelState) {
    tunnelRunning = state.running;
    tunnelURL = state.url || "";
    tunnelState.textContent = state.starting ? "Starting" : state.stopping ? "Stopping" : state.running ? "Online" : "Offline";
    tunnelState.className = state.running ? "state-online" : "state-offline";
    tunnelDetail.textContent = cloudflaredInstalled ? state.error || (state.running ? "Remote access enabled" : "Stopped") : "Install Cloudflared to enable remote access";
    tunnelAddress.hidden = !tunnelURL;
    tunnelAddressValue.textContent = tunnelURL || "-";
    toggleTunnelButton.textContent = state.running || state.starting ? "Stop" : "Start";
    toggleTunnelButton.disabled = state.starting || state.stopping || !cloudflaredInstalled;
    toggleTunnelButton.title = cloudflaredInstalled ? "" : "Install Cloudflared first";
    updatePairingCode();
}

function updatePairingCode() {
    if (!pairingToken) {
        return;
    }
    pairingValue.textContent = pairingToken;
    pairingExpiry.textContent = `Expires ${pairingExpiresAt}`;
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
        renderListenAddr(overview.listenAddr);
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
        const state = await AppService.ToggleTunnel();
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
        const state = await AppService.ToggleAppServer();
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
        const info = document.createElement("div");
        const label = document.createElement("span");
        label.textContent = device.name;
        const status = document.createElement("span");
        status.className = `device-state ${device.connected ? "online" : "offline"}`;
        status.textContent = `${device.connected ? "Connected" : "Offline"} · ${new Date(device.lastSeen).toLocaleString()}`;
        info.append(label, status);
        const revoke = document.createElement("button");
        revoke.className = "mini-button";
        revoke.textContent = "Revoke";
        revoke.addEventListener("click", () => void revokeDevice(device.id));
        item.append(info, revoke);
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
    await copyText(pairingToken, "Pairing code");
}

async function copyText(value: string, label: string) {
    if (!value) return;
    try {
        await Clipboard.SetText(value);
        message.textContent = `${label} copied`;
    } catch (error) {
        message.textContent = error instanceof Error ? error.message : `Unable to copy ${label.toLowerCase()}`;
    }
}

function renderListenAddr(address: string) {
    const separator = address.lastIndexOf(":");
    if (separator < 0) return;
    listenHost.value = address.slice(0, separator);
    listenPort.value = address.slice(separator + 1);
}

async function saveListenAddr() {
    if (!listenHost.reportValidity() || !listenPort.reportValidity()) return;
    saveListenButton.disabled = true;
    message.textContent = "Restarting local server...";
    try {
        const overview = await AppService.SetListenAddr(`${listenHost.value.trim()}:${listenPort.value}`);
        controlAddr = overview.controlAddr || "";
        renderListenAddr(overview.listenAddr);
        renderAppServer(overview.appServer);
        renderTunnel(overview.tunnel);
        message.textContent = "Local server restarted";
    } catch (error) {
        message.textContent = error instanceof Error ? error.message : "Unable to update listen address";
    } finally {
        saveListenButton.disabled = false;
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
    message.textContent = `${button.textContent === "Upgrade" ? "Upgrading" : "Installing"} ${label}...`;
    try {
        const result = kind === "node" ? await AppService.InstallNode() : kind === "codex" ? await AppService.InstallCodex() : await AppService.InstallCloudflared();
        render(result);
        renderTunnel(await AppService.TunnelState());
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
copyAppServerButton.addEventListener("click", () => void copyText(controlAddr, "Local address"));
copyTunnelButton.addEventListener("click", () => void copyText(tunnelURL, "Cloudflare address"));
saveListenButton.addEventListener("click", () => void saveListenAddr());
void refresh();
void refreshDevices();
window.setInterval(() => {
    void Promise.all([AppService.AppServerState(), AppService.TunnelState()]).then(([runtime, tunnel]) => { renderAppServer(runtime); renderTunnel(tunnel); }).catch(() => undefined);
    void refreshDevices();
}, 5000);
