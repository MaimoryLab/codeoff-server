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
GET /api/v1/status
POST /api/v1/pair/exchange
GET /api/v1/threads
POST /api/v1/threads
POST /api/v1/threads/:threadID/turns
POST /api/v1/turns/:turnID/interrupt?threadId=:threadID
POST /api/v1/approvals/:requestID
GET /api/v1/events
```

Pair exchange is the only public API route. All other `/api/v1` routes require a device `Bearer` token.

## Build

```sh
wails3 build
```

The generated binary is written to `bin/codexremote`.
