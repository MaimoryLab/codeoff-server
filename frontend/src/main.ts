import {AppService} from "../bindings/github.com/MaimoryLab/codex-server";
import type {Snapshot, ToolStatus} from "../bindings/github.com/MaimoryLab/codex-server/internal/diagnostics/models.js";
import {Clipboard} from "@wailsio/runtime";

type Language = "en" | "zh";

const translations: Record<Language, Record<string, string>> = {
    en: {
        language: "Language", english: "English", chinese: "简体中文", localControl: "LOCAL CONTROL", refresh: "Refresh", copy: "Copy",
        runtime: "RUNTIME", appServer: "Codex app-server", remoteAccess: "REMOTE ACCESS", cloudflareTunnel: "Cloudflare Tunnel", devices: "DEVICES", boundDevices: "Bound devices", bindDevice: "Bind new device", noDevices: "No devices bound", environment: "ENVIRONMENT", environmentStatus: "Environment status", localTools: "Local tools", checking: "Checking", install: "Install", saveRestart: "Save & restart", listenAddress: "Listen address", port: "Port",
        installed: "Installed", notInstalled: "Not installed", upgrade: "Upgrade", start: "Start", stop: "Stop", checked: "Checked {value}", starting: "Starting", stopping: "Stopping", running: "Running", offline: "Offline", stopped: "Stopped", online: "Online", localControlEndpoint: "Local control endpoint", remoteAccessEnabled: "Remote access enabled", installCloudflared: "Install Cloudflared to enable remote access", installCloudflaredFirst: "Install Cloudflared first", expires: "Expires {value}", connected: "Connected", revoke: "Revoke", copied: "{label} copied", unableToCopy: "Unable to copy {label}", connectionChecking: "Checking local environment...", unableToCheck: "Unable to check environment", startingTunnel: "Starting tunnel...", stoppingTunnel: "Stopping tunnel...", tunnelOnline: "Tunnel is online", tunnelStopped: "Tunnel stopped", unableChangeTunnel: "Unable to change tunnel state", startingAppServer: "Starting app-server...", stoppingAppServer: "Stopping app-server...", appServerRunning: "App-server is running", appServerStopped: "App-server stopped", unableChangeAppServer: "Unable to change app-server state", unableLoadDevices: "Unable to load devices", unableCreatePairing: "Unable to create pairing code", restartingServer: "Restarting local server...", serverRestarted: "Local server restarted", unableUpdateListen: "Unable to update listen address", unableRevoke: "Unable to revoke device", installing: "Installing {label}...", upgrading: "Upgrading {label}...", installationFailed: "Installation failed"
    },
    zh: {
        language: "语言", english: "English", chinese: "简体中文", localControl: "本地控制", refresh: "刷新", copy: "复制",
        runtime: "运行时", appServer: "Codex 应用服务", remoteAccess: "远程访问", cloudflareTunnel: "Cloudflare 隧道", devices: "设备", boundDevices: "已绑定设备", bindDevice: "绑定新设备", noDevices: "暂无绑定设备", environment: "环境", environmentStatus: "环境状态", localTools: "本地工具", checking: "检查中", install: "安装", saveRestart: "保存并重启", listenAddress: "监听地址", port: "端口",
        installed: "已安装", notInstalled: "未安装", upgrade: "升级", start: "启动", stop: "停止", checked: "检查于 {value}", starting: "启动中", stopping: "停止中", running: "运行中", offline: "离线", stopped: "已停止", online: "在线", localControlEndpoint: "本地控制端点", remoteAccessEnabled: "已启用远程访问", installCloudflared: "安装 Cloudflared 以启用远程访问", installCloudflaredFirst: "请先安装 Cloudflared", expires: "过期时间 {value}", connected: "已连接", revoke: "撤销", copied: "已复制{label}", unableToCopy: "无法复制{label}", connectionChecking: "正在检查本地环境...", unableToCheck: "无法检查环境", startingTunnel: "正在启动隧道...", stoppingTunnel: "正在停止隧道...", tunnelOnline: "隧道已上线", tunnelStopped: "隧道已停止", unableChangeTunnel: "无法更改隧道状态", startingAppServer: "正在启动应用服务...", stoppingAppServer: "正在停止应用服务...", appServerRunning: "应用服务运行中", appServerStopped: "应用服务已停止", unableChangeAppServer: "无法更改应用服务状态", unableLoadDevices: "无法加载设备", unableCreatePairing: "无法创建配对码", restartingServer: "正在重启本地服务...", serverRestarted: "本地服务已重启", unableUpdateListen: "无法更新监听地址", unableRevoke: "无法撤销设备", installing: "正在安装 {label}...", upgrading: "正在升级 {label}...", installationFailed: "安装失败"
    }
};

const languageSelect = document.querySelector<HTMLSelectElement>("#language")!;
const savedLanguage = localStorage.getItem("codex-language");
let language: Language = savedLanguage === "zh" ? "zh" : "en";

function t(key: string, args: Record<string, string> = {}) {
    let value = translations[language][key] || translations.en[key] || key;
    for (const [name, replacement] of Object.entries(args)) value = value.replace(`{${name}}`, replacement);
    return value;
}

function applyLanguage() {
    document.documentElement.lang = language === "zh" ? "zh-CN" : "en";
    languageSelect.value = language;
    document.querySelectorAll<HTMLElement>("[data-i18n]").forEach((element) => {
        const key = element.dataset.i18n;
        if (key) element.textContent = t(key);
    });
    document.querySelectorAll<HTMLElement>("[data-i18n-aria-label]").forEach((element) => {
        const key = element.dataset.i18nAriaLabel;
        if (key) element.setAttribute("aria-label", t(key));
    });
    renderCurrentState();
}

const checkedAt = document.querySelector<HTMLElement>("#checked-at")!;
const platform = document.querySelector<HTMLElement>("#platform")!;
const message = document.querySelector<HTMLElement>("#message")!;
const messageText = document.querySelector<HTMLElement>("#message-text")!;
const copyMessageButton = document.querySelector<HTMLButtonElement>("#copy-message")!;
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
let toastTimer = 0;
let lastSnapshot: Snapshot | null = null;
let lastAppServerState: RuntimeState | null = null;
let lastTunnelState: TunnelState | null = null;

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
    element.textContent = tool.installed ? tool.version || t("installed") : tool.error || t("notInstalled");
}

function showToast(text: string, error = false) {
    window.clearTimeout(toastTimer);
    message.hidden = !text;
    message.classList.toggle("error", error);
    messageText.textContent = text;
    copyMessageButton.hidden = !error;
    if (text) toastTimer = window.setTimeout(() => { message.hidden = true; }, error ? 10000 : 5000);
}

function render(snapshot: Snapshot) {
    lastSnapshot = snapshot;
    platform.textContent = `${snapshot.platform} / ${snapshot.architecture}`;
    checkedAt.textContent = t("checked", {value: new Date(snapshot.checkedAt).toLocaleTimeString()});
    renderTool(tools.node, snapshot.node);
    renderTool(tools.codex, snapshot.codex);
    renderTool(tools.cloudflared, snapshot.cloudflared);
    dashboard.classList.toggle("ready", snapshot.node.installed && snapshot.codex.installed);
    installNodeButton.disabled = false;
    installNodeButton.hidden = snapshot.node.installed;
    installCodexButton.disabled = false;
    installCodexButton.textContent = snapshot.codex.installed ? t("upgrade") : t("install");
    installCloudflaredButton.disabled = false;
    installCloudflaredButton.textContent = snapshot.cloudflared.installed ? t("upgrade") : t("install");
    cloudflaredInstalled = snapshot.cloudflared.installed;
    showToast("");
}

function renderAppServer(state: RuntimeState, address = controlAddr) {
    lastAppServerState = state;
    appServerRunning = state.running;
    appServerState.textContent = state.starting ? t("starting") : state.stopping ? t("stopping") : state.running ? t("running") : t("offline");
    appServerState.className = state.running ? "state-online" : "state-offline";
    appServerDetail.textContent = state.error || (state.running ? t("localControlEndpoint") : t("stopped"));
    appServerAddress.textContent = address || "-";
    copyAppServerButton.disabled = !address;
    toggleAppServerButton.textContent = state.running || state.starting ? t("stop") : t("start");
    toggleAppServerButton.disabled = state.starting || state.stopping;
}

function renderTunnel(state: TunnelState) {
    lastTunnelState = state;
    tunnelRunning = state.running;
    tunnelURL = state.url || "";
    tunnelState.textContent = state.starting ? t("starting") : state.stopping ? t("stopping") : state.running ? t("online") : t("offline");
    tunnelState.className = state.running ? "state-online" : "state-offline";
    tunnelDetail.textContent = cloudflaredInstalled ? state.error || (state.running ? t("remoteAccessEnabled") : t("stopped")) : t("installCloudflared");
    tunnelAddress.hidden = !tunnelURL;
    tunnelAddressValue.textContent = tunnelURL || "-";
    toggleTunnelButton.textContent = state.running || state.starting ? t("stop") : t("start");
    toggleTunnelButton.disabled = state.starting || state.stopping || !cloudflaredInstalled;
    toggleTunnelButton.title = cloudflaredInstalled ? "" : t("installCloudflaredFirst");
    updatePairingCode();
}

function renderCurrentState() {
    if (lastSnapshot) render(lastSnapshot);
    if (lastAppServerState) renderAppServer(lastAppServerState);
    if (lastTunnelState) renderTunnel(lastTunnelState);
}

function updatePairingCode() {
    if (!pairingToken) {
        return;
    }
    pairingValue.textContent = pairingToken;
    pairingExpiry.textContent = t("expires", {value: pairingExpiresAt});
}

function hidePairingCode() {
    pairingToken = "";
    pairingCode.hidden = true;
}

async function refresh() {
    refreshButton.disabled = true;
    showToast(t("connectionChecking"));
    try {
        const [environment, overview] = await Promise.all([AppService.RefreshStatus(), AppService.Overview()]);
        controlAddr = overview.controlAddr || "";
        renderListenAddr(overview.listenAddr);
        render(environment);
        renderAppServer(overview.appServer);
        renderTunnel(overview.tunnel);
        await refreshDevices();
    } catch (error) {
        showToast(error instanceof Error ? error.message : t("unableToCheck"), true);
    } finally {
        refreshButton.disabled = false;
    }
}

async function toggleTunnel() {
    toggleTunnelButton.disabled = true;
    showToast(tunnelRunning ? t("stoppingTunnel") : t("startingTunnel"));
    try {
        const state = await AppService.ToggleTunnel();
        renderTunnel(state);
        showToast(state.running ? t("tunnelOnline") : t("tunnelStopped"));
    } catch (error) {
        showToast(error instanceof Error ? error.message : t("unableChangeTunnel"), true);
        renderTunnel(await AppService.TunnelState());
    }
}

async function toggleAppServer() {
    toggleAppServerButton.disabled = true;
    showToast(appServerRunning ? t("stoppingAppServer") : t("startingAppServer"));
    try {
        const state = await AppService.ToggleAppServer();
        renderAppServer(state);
        showToast(state.running ? t("appServerRunning") : t("appServerStopped"));
    } catch (error) {
        showToast(error instanceof Error ? error.message : t("unableChangeAppServer"), true);
        renderAppServer(await AppService.AppServerState());
    }
}

function renderDevices(devices: Device[]) {
    deviceList.replaceChildren();
    if (devices.length === 0) {
        const empty = document.createElement("li");
        empty.className = "muted";
        empty.textContent = t("noDevicesBound");
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
        status.textContent = `${device.connected ? t("connected") : t("offline")} · ${new Date(device.lastSeen).toLocaleString()}`;
        info.append(label, status);
        const revoke = document.createElement("button");
        revoke.className = "mini-button";
        revoke.textContent = t("revoke");
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
        showToast(error instanceof Error ? error.message : t("unableLoadDevices"), true);
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
        showToast(error instanceof Error ? error.message : t("unableCreatePairing"), true);
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
        showToast(t("copied", {label}));
    } catch (error) {
        showToast(error instanceof Error ? error.message : t("unableToCopy", {label: label.toLowerCase()}), true);
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
    showToast(t("restartingServer"));
    try {
        const overview = await AppService.SetListenAddr(`${listenHost.value.trim()}:${listenPort.value}`);
        controlAddr = overview.controlAddr || "";
        renderListenAddr(overview.listenAddr);
        renderAppServer(overview.appServer);
        renderTunnel(overview.tunnel);
        showToast(t("serverRestarted"));
    } catch (error) {
        showToast(error instanceof Error ? error.message : t("unableUpdateListen"), true);
    } finally {
        saveListenButton.disabled = false;
    }
}

async function revokeDevice(id: string) {
    try {
        await AppService.RevokeDevice(id);
        await refreshDevices();
    } catch (error) {
        showToast(error instanceof Error ? error.message : t("unableRevoke"), true);
    }
}

async function install(kind: "node" | "codex" | "cloudflared") {
    const button = kind === "node" ? installNodeButton : kind === "codex" ? installCodexButton : installCloudflaredButton;
    button.disabled = true;
    const label = kind === "node" ? "Node.js" : kind === "codex" ? "Codex CLI" : "Cloudflared";
    showToast(button.textContent === t("upgrade") ? t("upgrading", {label}) : t("installing", {label}));
    try {
        const result = kind === "node" ? await AppService.InstallNode() : kind === "codex" ? await AppService.InstallCodex() : await AppService.InstallCloudflared();
        render(result);
        renderTunnel(await AppService.TunnelState());
    } catch (error) {
        showToast(error instanceof Error ? error.message : t("installationFailed"), true);
        button.disabled = false;
    }
}

refreshButton.addEventListener("click", refresh);
languageSelect.addEventListener("change", () => {
    language = languageSelect.value === "zh" ? "zh" : "en";
    localStorage.setItem("codex-language", language);
    applyLanguage();
});
installNodeButton.addEventListener("click", () => void install("node"));
installCodexButton.addEventListener("click", () => void install("codex"));
installCloudflaredButton.addEventListener("click", () => void install("cloudflared"));
toggleAppServerButton.addEventListener("click", () => void toggleAppServer());
toggleTunnelButton.addEventListener("click", () => void toggleTunnel());
bindDeviceButton.addEventListener("click", () => void bindDevice());
copyPairingButton.addEventListener("click", () => void copyPairingCode());
copyMessageButton.addEventListener("click", () => void copyText(messageText.textContent ?? "", "Error"));
copyAppServerButton.addEventListener("click", () => void copyText(controlAddr, "Local address"));
copyTunnelButton.addEventListener("click", () => void copyText(tunnelURL, "Cloudflare address"));
saveListenButton.addEventListener("click", () => void saveListenAddr());
applyLanguage();
void refresh();
void refreshDevices();
window.setInterval(() => {
    void Promise.all([AppService.AppServerState(), AppService.TunnelState()]).then(([runtime, tunnel]) => { renderAppServer(runtime); renderTunnel(tunnel); }).catch(() => undefined);
    void refreshDevices();
}, 5000);
