# Codex Remote

Codex Remote is a local-first desktop bridge for controlling Codex from a phone. The Wails 3 desktop process owns the local HTTP API, environment checks, Codex process, and Cloudflare Tunnel.

## Requirements

- Go 1.24+
- Node.js and npm for the frontend
- Wails 3 CLI pinned to `v3.0.0-beta.11`

Install the pinned CLI once:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.11
```

## Development

```sh
npm --prefix frontend install
wails3 dev
```

The current desktop panel checks `node`, `codex`, `codex app-server`, and `cloudflared`. The local control API binds to an ephemeral loopback port and exposes:

```text
GET /healthz
GET /api/v1/status
```

## Build

```sh
wails3 build
```

The generated binary is written to `bin/codexremote`.
