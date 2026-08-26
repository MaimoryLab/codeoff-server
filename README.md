# Codeoff Server

Codeoff Server is a local-first desktop bridge for controlling Codex from a phone. The Wails 3 desktop process owns the local HTTP API, environment checks, Codex process, and Cloudflare Tunnel.

## Requirements

- Go 1.24+
- Node.js and pnpm for the frontend
- Wails 3 CLI pinned to `v3.0.0-beta.13`

Install the pinned CLI once:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.13
```

## Development

```sh
pnpm --dir frontend install
wails3 dev
```

The current desktop panel checks `node`, `codex`, `codex app-server`, and `cloudflared`. It can install Node.js through the available platform package manager (Homebrew, winget, or apt), Codex through npm, and Cloudflared through Homebrew or winget after an explicit button click. The local control API binds to an ephemeral port on all IPv4 interfaces; startup logs print the port for LAN clients, and the API exposes:

```text
POST /api/v1/pair/exchange
GET /api/v1/ws
```

Pair exchange is the only mobile pairing HTTP API. All mobile authenticated requests and server events share the `/api/v1/ws` WebSocket session and use request IDs for responses. The WebSocket handshake requires a device `Bearer` token. The CLI daemon also exposes token-protected local admin endpoints for lifecycle operations.

## Build

```sh
wails3 build
```

The generated binary is written to `bin/codeoff-server`.

## CLI daemon

The Wails-free version is split into `codeoff-daemon` and `codeoff-cli`. The
daemon uses the already-installed `codex` and `cloudflared` binaries and does
not install or upgrade anything.

```sh
go build -o bin/codeoff-daemon ./cmd/codeoff-daemon
go build -o bin/codeoff-cli ./cmd/codeoff-cli
bin/codeoff-daemon --listen 0.0.0.0:11037 --cf-tunnel --cf-tunnel-mode quick
```

Use `--cf-tunnel-mode external --cf-tunnel-url https://example.com` when a
Cloudflare application already publishes the control endpoint. The daemon
writes its local management address and token to its state file; the CLI reads
that file by default.

```sh
bin/codeoff-cli status
bin/codeoff-cli pair
bin/codeoff-cli devices
bin/codeoff-cli restart appserver
bin/codeoff-cli restart tunnel
```

The state file can be overridden on both commands with `--state PATH`.
