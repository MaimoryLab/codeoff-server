import {AppService} from "../bindings/github.com/MaimoryLab/codex-server";
import type {Snapshot, ToolStatus} from "../bindings/github.com/MaimoryLab/codex-server/internal/diagnostics/models.js";

const checkedAt = document.querySelector<HTMLElement>("#checked-at")!;
const platform = document.querySelector<HTMLElement>("#platform")!;
const message = document.querySelector<HTMLElement>("#message")!;
const refreshButton = document.querySelector<HTMLButtonElement>("#refresh")!;
const installNodeButton = document.querySelector<HTMLButtonElement>("#install-node")!;
const installCodexButton = document.querySelector<HTMLButtonElement>("#install-codex")!;
const appServerState = document.querySelector<HTMLElement>("#app-server-state")!;
const appServerDetail = document.querySelector<HTMLElement>("#app-server-detail")!;
const toggleAppServerButton = document.querySelector<HTMLButtonElement>("#toggle-app-server")!;
let appServerRunning = false;

type RuntimeState = {
    running: boolean;
    starting: boolean;
    startedAt: string;
    codexHome?: string;
    userAgent?: string;
    error?: string;
};

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
    message.textContent = "Environment check complete";
}

function renderAppServer(state: RuntimeState) {
    appServerRunning = state.running;
    appServerState.textContent = state.starting ? "Starting" : state.running ? "Running" : "Offline";
    appServerState.className = state.running ? "state-online" : "state-offline";
    appServerDetail.textContent = state.error || state.codexHome || "Stopped";
    toggleAppServerButton.textContent = state.running ? "Stop" : "Start";
    toggleAppServerButton.disabled = state.starting;
}

async function refresh() {
    refreshButton.disabled = true;
    message.textContent = "Checking local environment...";
    try {
        const [environment, runtime] = await Promise.all([AppService.RefreshStatus(), AppService.AppServerState()]);
        render(environment);
        renderAppServer(runtime);
    } catch (error) {
        message.textContent = error instanceof Error ? error.message : "Unable to check environment";
    } finally {
        refreshButton.disabled = false;
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

async function install(kind: "node" | "codex") {
    const button = kind === "node" ? installNodeButton : installCodexButton;
    button.disabled = true;
    message.textContent = `Installing ${kind === "node" ? "Node.js" : "Codex CLI"}...`;
    try {
        render(kind === "node" ? await AppService.InstallNode() : await AppService.InstallCodex());
    } catch (error) {
        message.textContent = error instanceof Error ? error.message : "Installation failed";
        button.disabled = false;
    }
}

refreshButton.addEventListener("click", refresh);
installNodeButton.addEventListener("click", () => void install("node"));
installCodexButton.addEventListener("click", () => void install("codex"));
toggleAppServerButton.addEventListener("click", () => void toggleAppServer());
void refresh();
window.setInterval(() => void AppService.AppServerState().then(renderAppServer).catch(() => undefined), 5000);
