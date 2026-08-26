<p align="center">
  <img src="build/appicon.png" alt="Codeoff Server logo" width="128">
</p>

<h1 align="center">Codeoff Server</h1>

<p align="center">
  <a href="https://github.com/MaimoryLab/codeoff-server/actions/workflows/ci-test.yml"><img src="https://github.com/MaimoryLab/codeoff-server/actions/workflows/ci-test.yml/badge.svg" alt="Go CI"></a>
  <a href="https://github.com/MaimoryLab/codeoff-server/actions/workflows/build-artifacts.yml"><img src="https://github.com/MaimoryLab/codeoff-server/actions/workflows/build-artifacts.yml/badge.svg" alt="Build artifacts"></a>
  <a href="README.md">English</a>
</p>

Codeoff Server 是一个本地优先的桌面桥接服务，用于通过手机控制 Codex。Wails 3 应用负责本地控制 API、环境检查、Codex app-server 进程和 Cloudflare Tunnel。将它与 [Codeoff Mobile](https://github.com/MaimoryLab/codeoff) 配对，即可在移动设备上工作。

## 快速开始

### 环境要求

- Go `1.27+`
- Node.js 和 pnpm
- Wails 3 CLI `v3.0.0-beta.13`
- 已安装的 `codex` 和 `cloudflared` 可执行文件（桌面面板支持在用户明确点击后安装部分依赖）

先安装固定版本的 Wails CLI：

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.13
```

### 运行桌面应用

```sh
pnpm --dir frontend install
wails3 dev
```

在桌面面板中启动 Codex app-server 和 Cloudflare Tunnel，然后将显示的 Tunnel 地址和一次性配对码输入 Codeoff Mobile。

本地控制 API 提供 `POST /api/v1/pair/exchange` 用于配对，以及 `GET /api/v1/ws` 用于鉴权后的 WebSocket 会话。移动端请求和服务端事件共用该 WebSocket 连接。

### 构建

```sh
wails3 build
```

生成的二进制文件位于 `bin/codeoff-server`。

### 运行 CLI daemon（可选）

无需 Wails 界面的 daemon 会直接使用已安装的 `codex` 和 `cloudflared`：

```sh
go build -o bin/codeoff-daemon ./cmd/codeoff-daemon
go build -o bin/codeoff-cli ./cmd/codeoff-cli
bin/codeoff-daemon --listen 0.0.0.0:11037 --cf-tunnel --cf-tunnel-mode quick
```

然后使用 CLI 查看或控制 daemon：

```sh
bin/codeoff-cli status
bin/codeoff-cli pair
bin/codeoff-cli devices
bin/codeoff-cli restart appserver
bin/codeoff-cli restart tunnel
```

已有 Cloudflare 应用 Tunnel 时，使用 `--cf-tunnel-mode external --cf-tunnel-url https://example.com`。可通过 `--state PATH` 覆盖 daemon 状态文件路径。

## 开发检查

```sh
go vet ./...
go test -race ./...
staticcheck ./...
```

## 相关项目

- [Codeoff Mobile](https://github.com/MaimoryLab/codeoff)：桌面桥接服务的 Flutter 客户端。

## 许可证

[Apache License 2.0](LICENSE)
