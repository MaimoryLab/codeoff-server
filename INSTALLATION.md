# Codeoff Server 安装与配对指南

本文是给安装者和 AI agent 的可执行 runbook：先让桌面服务可用，再选择局域网或 Cloudflare Tunnel，最后引导 Codeoff Mobile 配对。

## 1. 安装前检查

- 支持 macOS、Windows、Linux。下载包见 [最新 Releases](https://github.com/MaimoryLab/codeoff-server/releases/latest)。
- 必需：Node.js、Codex CLI，且 `codex` 在启动 Codeoff Server 的用户 PATH 中。
- 可选：`cloudflared`。只有手机需要从局域网外连接时才需要；局域网连接不需要。
- 服务端和手机必须使用兼容版本。遇到“需要升级”时，先同时更新两端。

## 2. 安装桌面版

1. macOS 下载匹配架构的 `.dmg`（Apple Silicon 选 `arm64`，Intel 选 `amd64`）；Windows 下载对应的 `-installer.exe`；Linux 下载对应架构的 `.deb`、`.rpm` 或 `.pkg.tar.zst`。
2. 安装并启动 Codeoff Server。
3. 在 **Environment status** 点击 **Refresh**，确认 Node.js 和 Codex 为已安装。面板可在用户明确点击后安装或升级受支持的依赖。
4. 如果状态显示找不到 `codex`，把 Codex CLI 加入登录 shell 的 PATH 后完全退出并重新启动桌面应用。

## 3. 配置并启动服务

### 仅局域网

1. 在监听地址设置中使用 `0.0.0.0:11037`（或绑定到电脑的局域网 IP）。默认 `127.0.0.1:11037` 只能本机访问。
2. 在面板启动 **Codex app-server**。
3. 放行操作系统防火墙的 TCP `11037`，手机与电脑连接同一 Wi-Fi。

### 远程访问

1. 安装 `cloudflared`，或在面板的环境状态中安装。
2. 选择 **Quick Tunnel** 并启动 Tunnel；面板会显示一个 `https://*.trycloudflare.com` 地址。该地址每次启动可能变化。
3. 需要稳定地址时选择 **Custom domain**，填写已有 Cloudflare Tunnel 的 HTTPS URL，保存后再启动 Tunnel。
4. 同时启动 Codex app-server。配对二维码会自动包含可用的局域网地址和 Tunnel 地址。

### 可选：CLI daemon

无桌面环境时，从源码构建并运行：

```sh
go build -o bin/codeoff-daemon ./cmd/codeoff-daemon
go build -o bin/codeoff-cli ./cmd/codeoff-cli
bin/codeoff-daemon --listen 0.0.0.0:11037
```

远程 Quick Tunnel：

```sh
bin/codeoff-daemon --listen 0.0.0.0:11037 --cf-tunnel --cf-tunnel-mode quick
```

已有 Cloudflare Tunnel：

```sh
bin/codeoff-daemon --listen 0.0.0.0:11037 --cf-tunnel \
  --cf-tunnel-mode external --cf-tunnel-url https://example.example.com
```

用 CLI 查看状态、生成二维码或撤销设备：

```sh
bin/codeoff-cli status
bin/codeoff-cli pair                 # 输出一次性配对码和二维码
bin/codeoff-cli --qr-dir ./qr pair   # 将二维码 PNG 写入 ./qr
bin/codeoff-cli connect              # 只生成连接二维码（已配对设备）
bin/codeoff-cli devices
bin/codeoff-cli revoke DEVICE_ID
```

daemon 默认监听 `127.0.0.1:11037`；`--listen`、`--cf-tunnel`、`--cf-tunnel-mode`、`--cf-tunnel-url`、`--state`、`--codex` 和 `--cloudflared` 可覆盖默认值。

## 4. 引导手机配对

1. 在面板点击 **Bind new device**（CLI 使用 `codeoff-cli pair`）。配对码有效 10 分钟，成功交换后立即失效。
2. 首选二维码：让用户在 Codeoff Mobile 点二维码扫描按钮并扫描当前二维码。二维码包含服务 UUID、监听地址、Tunnel 地址和一次性配对码，应用会自动选择可达地址。
3. 手动方式：把面板显示的监听地址或 Tunnel 地址、配对码交给用户；用户在 App 输入地址后点 **Connect**，再输入配对码和设备名称。
4. 配对成功后，App 保存设备令牌并自动建立连接。不要把设备令牌或 daemon 管理令牌写入聊天、日志或工单。

## 5. 连接后的运维

- 面板 **Devices** 可查看在线设备；设备遗失或不再使用时立即 **Revoke**。
- Tunnel 重启后 Quick Tunnel URL 可能变化；重新生成连接二维码即可，已保存的设备令牌仍可复用。
- 长时间任务需要无人值守时，在菜单栏启用 **Prevent system sleep**；它只在 app-server 运行期间生效。

## 6. 故障排查

- **手机无法连接局域网地址**：确认服务监听 `0.0.0.0` 或局域网 IP、两端在同一网络、TCP `11037` 未被防火墙拦截。
- **Tunnel 地址不可达**：确认 `cloudflared` 已安装且 Tunnel 状态为在线；Quick Tunnel 启动后等待地址出现，再重新打开配对窗口。
- **配对码无效/过期**：关闭旧窗口，重新生成配对码；不要重复使用已成功交换的码。
- **需要升级**：更新 Codeoff Server 和 Codeoff Mobile 到兼容版本后再连接。
- **设备令牌失效**：在服务端撤销该设备，再生成新的配对码重新绑定。

## 给 AI agent 的执行规则

1. 先询问用户的主机系统、手机系统和网络位置（同一局域网还是跨网络），据此选择局域网或 Tunnel，不默认公开服务。
2. 按“检查 Node.js/Codex → 启动 app-server → 配置监听/Tunnel → 生成配对码 → 指引 App 扫码”的顺序推进；每一步先确认状态再继续。
3. 优先二维码，手动流程只传递地址、一次性配对码和设备名称；绝不索取或回显设备令牌、admin token 或 `devices.json` 内容。
4. 连接失败时先检查地址可达性和版本，再重新生成未过期的配对码；不要让用户反复提交同一个配对码。
