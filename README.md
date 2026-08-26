<p align="center">
  <img src="build/appicon.png" alt="Codeoff Server logo" width="128">
</p>

<h1 align="center">Codeoff Server</h1>

<p align="center">
  <a href="https://github.com/MaimoryLab/codeoff-server/actions/workflows/ci-test.yml"><img src="https://github.com/MaimoryLab/codeoff-server/actions/workflows/ci-test.yml/badge.svg" alt="Go CI"></a>
  <a href="https://github.com/MaimoryLab/codeoff-server/actions/workflows/build-artifacts.yml"><img src="https://github.com/MaimoryLab/codeoff-server/actions/workflows/build-artifacts.yml/badge.svg" alt="Build artifacts"></a>
  <a href="README.zh-CN.md">中文</a>
</p>

Codeoff Server is a local-first desktop bridge for controlling Codex from a phone. The Wails 3 application owns the local control API, environment checks, Codex app-server process, and Cloudflare Tunnel. Pair it with [Codeoff Mobile](https://github.com/MaimoryLab/codeoff) to work from a mobile device.

## Quickstart

### Requirements

- Go `1.27+`
- Node.js and pnpm
- Wails 3 CLI `v3.0.0-beta.13`
- Installed `codex` and `cloudflared` binaries (the desktop panel can install supported dependencies after an explicit action)

Install the pinned Wails CLI once:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.13
```

### Run the desktop app

```sh
pnpm --dir frontend install
wails3 dev
```

In the desktop panel, start the Codex app-server and Cloudflare Tunnel. Copy the displayed Tunnel URL and one-time pairing code into Codeoff Mobile.

The local control API exposes `POST /api/v1/pair/exchange` for pairing and `GET /api/v1/ws` for authenticated WebSocket sessions. Mobile requests and server events share that WebSocket connection.

### Build

```sh
wails3 build
```

The generated binary is written to `bin/codeoff-server`.

### Run the CLI daemon (optional)

The Wails-free daemon uses already-installed `codex` and `cloudflared` binaries:

```sh
go build -o bin/codeoff-daemon ./cmd/codeoff-daemon
go build -o bin/codeoff-cli ./cmd/codeoff-cli
bin/codeoff-daemon --listen 0.0.0.0:11037 --cf-tunnel --cf-tunnel-mode quick
```

Then inspect or control it with:

```sh
bin/codeoff-cli status
bin/codeoff-cli pair
bin/codeoff-cli devices
bin/codeoff-cli restart appserver
bin/codeoff-cli restart tunnel
```

Use `--cf-tunnel-mode external --cf-tunnel-url https://example.com` for an existing Cloudflare application tunnel. Override the daemon state file with `--state PATH`.

## Development checks

```sh
go vet ./...
go test -race ./...
staticcheck ./...
```

## Related project

- [Codeoff Mobile](https://github.com/MaimoryLab/codeoff): Flutter client for the desktop bridge.

## License

[Apache License 2.0](LICENSE)
