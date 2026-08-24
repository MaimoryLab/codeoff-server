# Codex Remote

Codex Remote is a local-first desktop bridge for controlling Codex from a phone. The Wails 3 desktop process owns the local HTTP API, environment checks, Codex process, and Cloudflare Tunnel.

## Requirements

- Go 1.24+
- Node.js and pnpm for the frontend
- Wails 3 CLI pinned to `v3.0.0-beta.11`

Install the pinned CLI once:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.11
```

## Development

```sh
pnpm --dir frontend install
wails3 dev
```

The current desktop panel checks `node`, `codex`, `codex app-server`, and `cloudflared`. It can install Node.js through the available platform package manager (Homebrew, winget, or apt), Codex through npm, and Cloudflared through Homebrew or winget after an explicit button click. The local control API binds to an ephemeral port on all IPv4 interfaces; startup logs print the port for LAN clients, and the API exposes:

```text
GET /healthz
POST /api/v1/pair/exchange
GET /api/v1/ws
```

Pair exchange is the only HTTP API. All authenticated requests and server events share the `/api/v1/ws` WebSocket session and use request IDs for responses. The WebSocket handshake requires a device `Bearer` token.

## Build

```sh
wails3 build
```

The generated binary is written to `bin/codexremote`.
