<p align="center">
  <img src="build/appicon.png" alt="Codeoff Server logo" width="128">
</p>

<h1 align="center">Codeoff Server</h1>

<p align="center">
  <a href="https://github.com/MaimoryLab/codeoff-server/actions/workflows/ci-test.yml"><img src="https://github.com/MaimoryLab/codeoff-server/actions/workflows/ci-test.yml/badge.svg" alt="Go CI"></a>
  <a href="https://github.com/MaimoryLab/codeoff-server/actions/workflows/build-artifacts.yml"><img src="https://github.com/MaimoryLab/codeoff-server/actions/workflows/build-artifacts.yml/badge.svg" alt="Build artifacts"></a>
  <a href="https://github.com/MaimoryLab/codeoff-server/releases/latest"><img src="https://img.shields.io/github/v/release/MaimoryLab/codeoff-server?display_name=tag&sort=semver" alt="Latest release"></a>
  <a href="https://github.com/MaimoryLab/codeoff-server"><img src="https://img.shields.io/github/stars/MaimoryLab/codeoff-server?style=flat&label=Codeoff%20Server" alt="Codeoff Server repository"></a>
  <a href="README.md">English</a>
</p>

Codeoff Server 是一个本地优先的桌面桥接服务，用于通过手机控制 Codex。Wails 3 应用负责本地控制 API、环境检查、Codex app-server 进程和 Cloudflare Tunnel。将它与 [Codeoff Mobile](https://github.com/MaimoryLab/codeoff) 配对，即可在移动设备上工作。

## 工作原理

1. 桌面应用检查本地依赖，启动 Codex app-server，并绑定控制 API。
2. Cloudflare Tunnel 发布控制 API 地址，桌面面板显示一次性配对码。
3. Codeoff Mobile 通过 `POST /api/v1/pair/exchange` 交换配对码，再使用返回的设备令牌鉴权连接 `/api/v1/ws` WebSocket。
4. 服务端将线程、turn、上传和审批操作转发给 Codex app-server，并把事件流推送给已连接设备。
5. 可选的 CLI daemon 复用同一套控制组件，不需要 Wails 桌面界面。

## 快速开始

### 使用

#### 下载 release

从 [Releases](https://github.com/MaimoryLab/codeoff-server/releases/latest) 下载最新安装包：

- macOS：选择匹配 `amd64`（Intel）或 `arm64`（Apple Silicon）的 `.dmg`。
- Windows：选择对应架构的 `-installer.exe`。
- Linux：选择对应架构的 `.deb`、`.rpm` 或 `.pkg.tar.zst`。

Release 还提供名为 `ota-codeoff-server-<platform>-<arch>.zip` 的各平台 OTA 压缩包。

安装并启动 Codeoff Server，在桌面面板中启动 Codex app-server 和 Cloudflare Tunnel，然后将显示的 Tunnel 地址和一次性配对码输入 Codeoff Mobile。

#### CLI daemon（可选）

Release 安装包对应桌面应用。无 Wails 界面的部署方式需要按下方开发章节从源码构建 daemon 和 CLI。

### 开发

#### 环境要求

- Go `1.27+`
- Node.js 和 pnpm
- Wails 3 CLI `v3.0.0-beta.13`
- 已安装的 `codex` 和 `cloudflared` 可执行文件（桌面面板支持在用户明确点击后安装部分依赖）

先安装固定版本的 Wails CLI：

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.13
```

#### 运行桌面应用

```sh
pnpm --dir frontend install
wails3 dev
```

#### 构建与检查

```sh
wails3 build
go vet ./...
go test -race ./...
staticcheck ./...
```

生成的桌面二进制文件位于 `bin/codeoff-server`。

#### 构建并运行 CLI daemon

```sh
go build -o bin/codeoff-daemon ./cmd/codeoff-daemon
go build -o bin/codeoff-cli ./cmd/codeoff-cli
bin/codeoff-daemon --listen 0.0.0.0:11037 --cf-tunnel --cf-tunnel-mode quick
bin/codeoff-cli status
bin/codeoff-cli connect
bin/codeoff-cli pair
bin/codeoff-cli devices
bin/codeoff-cli restart appserver
bin/codeoff-cli restart tunnel
```

CLI 默认输出人类可读文本，添加 `--json` 可输出机器可读 JSON。`connect` 会将连接二维码 PNG、`pair` 会将配对二维码 PNG 写入当前目录，也可以通过 `--qr-dir PATH` 指定输出目录。生成二维码需要安装 `qrencode` 命令。

已有 Cloudflare 应用 Tunnel 时，使用 `--cf-tunnel-mode external --cf-tunnel-url https://example.com`。可通过 `--state PATH` 覆盖 daemon 状态文件路径。

## 相关项目

- [Codeoff Mobile](https://github.com/MaimoryLab/codeoff)：桌面桥接服务的 Flutter 客户端。

## 许可证

[Apache License 2.0](LICENSE)
