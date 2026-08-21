import {AppService} from "../bindings/github.com/MaimoryLab/codex-server";
import type {Snapshot, ToolStatus} from "../bindings/github.com/MaimoryLab/codex-server/internal/diagnostics/models.js";

const checkedAt = document.querySelector<HTMLElement>("#checked-at")!;
const platform = document.querySelector<HTMLElement>("#platform")!;
const message = document.querySelector<HTMLElement>("#message")!;
const refreshButton = document.querySelector<HTMLButtonElement>("#refresh")!;

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
    message.textContent = "Environment check complete";
}

async function refresh() {
    refreshButton.disabled = true;
    message.textContent = "Checking local environment...";
    try {
        render(await AppService.RefreshStatus());
    } catch (error) {
        message.textContent = error instanceof Error ? error.message : "Unable to check environment";
    } finally {
        refreshButton.disabled = false;
    }
}

refreshButton.addEventListener("click", refresh);
void refresh();
