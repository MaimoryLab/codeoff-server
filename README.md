<p align="center">
  <img src="build/appicon.png" alt="Codeoff Server logo" width="128">
</p>

<h1 align="center">Codeoff Server</h1>

<p align="center">
  <a href="https://github.com/MaimoryLab/codeoff-server/actions/workflows/ci-test.yml"><img src="https://github.com/MaimoryLab/codeoff-server/actions/workflows/ci-test.yml/badge.svg" alt="Go CI"></a>
  <a href="https://github.com/MaimoryLab/codeoff-server/actions/workflows/build-artifacts.yml"><img src="https://github.com/MaimoryLab/codeoff-server/actions/workflows/build-artifacts.yml/badge.svg" alt="Build artifacts"></a>
  <a href="https://github.com/MaimoryLab/codeoff-server/releases/latest"><img src="https://img.shields.io/github/v/release/MaimoryLab/codeoff-server?display_name=tag&sort=semver" alt="Latest release"></a>
  <a href="https://github.com/MaimoryLab/codeoff-server"><img src="https://img.shields.io/github/stars/MaimoryLab/codeoff-server?style=flat&label=Codeoff%20Server" alt="Codeoff Server repository"></a>
  <a href="README.zh-CN.md">中文</a>
</p>

Codeoff Server is a local-first desktop bridge for controlling Codex from a phone. The Wails 3 application owns the local control API, environment checks, Codex app-server process, and Cloudflare Tunnel. Pair it with [Codeoff Mobile](https://github.com/MaimoryLab/codeoff) to work from a mobile device.

## How it works

1. The desktop app checks local dependencies, starts the Codex app-server, and binds the control API.
2. Cloudflare Tunnel publishes the control API origin and the panel shows a one-time pairing code.
3. Codeoff Mobile exchanges that code at `POST /api/v1/pair/exchange`, then authenticates the `/api/v1/ws` WebSocket with the returned device token.
4. The server forwards thread, turn, upload, and approval operations to the Codex app-server and streams its events back to connected devices.
5. The optional CLI daemon uses the same control components without the Wails desktop UI.

## Quickstart

### Use

#### Download a release

Download the latest package from [Releases](https://github.com/MaimoryLab/codeoff-server/releases/latest):

- macOS: choose the `.dmg` matching `amd64` (Intel) or `arm64` (Apple Silicon).
- Windows: choose the matching `-installer.exe`.
- Linux: choose the matching `.deb`, `.rpm`, or `.pkg.tar.zst`.

The release also includes platform OTA archives named `ota-codeoff-server-<platform>-<arch>.zip`.

Install and launch Codeoff Server, start the Codex app-server and Cloudflare Tunnel in the desktop panel, then copy the displayed Tunnel URL and one-time pairing code into Codeoff Mobile.

#### CLI daemon (optional)

Release packages are the desktop app. For a Wails-free deployment, build the daemon and CLI from source as described below.

### Develop

#### Requirements

- Go `1.27+`
- Node.js and pnpm
- Wails 3 CLI `v3.0.0-beta.13`
- Installed `codex` and `cloudflared` binaries (the desktop panel can install supported dependencies after an explicit action)

Install the pinned Wails CLI once:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.13
```

#### Run the desktop app

```sh
pnpm --dir frontend install
wails3 dev
```

#### Build and check

```sh
wails3 build
go vet ./...
go test -race ./...
staticcheck ./...
```

The generated desktop binary is written to `bin/codeoff-server`.

#### Build and run the CLI daemon

```sh
go build -o bin/codeoff-daemon ./cmd/codeoff-daemon
go build -o bin/codeoff-cli ./cmd/codeoff-cli
bin/codeoff-daemon --listen 0.0.0.0:11037 --cf-tunnel --cf-tunnel-mode quick
bin/codeoff-cli status
bin/codeoff-cli pair
bin/codeoff-cli connect
bin/codeoff-cli devices
bin/codeoff-cli restart appserver
bin/codeoff-cli restart tunnel
```

CLI output is human-readable by default. Add `--json` for machine-readable output; `pair` and `connect` write QR PNG files to the current directory, or to a directory selected with `--qr-dir PATH`. QR generation requires the `qrencode` command.

Use `--cf-tunnel-mode external --cf-tunnel-url https://example.com` for an existing Cloudflare application tunnel. Override the daemon state file with `--state PATH`.

## Related project

- [Codeoff Mobile](https://github.com/MaimoryLab/codeoff): Flutter client for the desktop bridge.

## License

[Apache License 2.0](LICENSE)
