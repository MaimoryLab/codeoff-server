# Codex 0.155.1 protocol compatibility

Checked on 2026-09-20 against the installed `codex-cli 0.155.1` executable:

```sh
codex app-server generate-ts --experimental --out /tmp/codex-0.155.1-types
codex app-server generate-json-schema --experimental --out /tmp/codex-0.155.1-schema
CODEOFF_CODEX_TEST=1 go test ./internal/appserver -run TestCodexProtocol -v
```

The integration test uses an isolated Codex home and a local HTTP provider that
rejects requests. It exercises real protocol handling and persisted user messages
without contacting an external model provider or using the user's sessions or credentials.

| Contract | Codeoff behavior |
| --- | --- |
| `RequestId` is a string or integer | Preserve the JSON type and integer precision through stdio, WebSocket events, and approval replies. |
| Permission approval results contain `permissions` and `scope` | Translate the user's decision using the original request. Grant only requested permissions; default to the current turn. Denial grants nothing. |
| Legacy exec/patch approvals use `ReviewDecision` | Translate allow/deny and preserve structured policy amendments. |
| `serverRequest/resolved` ends a pending request | Remove server-side approval metadata and the mobile approval card. |
| `currentTime/read` expects whole Unix seconds | Answer locally with `currentTimeAt`. |
| Text input includes `text_elements` | Send an empty array when there are no annotated spans. |
| `historyMode: paginated` uses cursor reads | Inspect metadata, request `thread/turns/list` with `itemsView: full` and ascending order, and follow `nextCursor` until exhausted. Keep the existing mobile `thread.turns` shape. |
| Resume can exclude full history | Send `excludeTurns: true`; the separate history read hydrates messages. |
| JSON-RPC errors have different meanings | Distinguish writer conflicts, invalid requests, unsupported methods, overload, and upstream failures. Unsupported methods do not disconnect the mobile client. |

Both legacy and paginated threads with persisted messages passed the real-binary
history and resume checks. The fake-transport tests also cover multiple pages,
complete item content, cursor loops, and upstream failures.

Before the first user message, Codex can report that a thread is not materialized.
Codeoff returns its metadata with empty history for that specific error. Early
probes also observed `-32601` / `list_turns is not supported yet` on newly created
empty threads; this does **not** mean that 0.155.1 lacks pagination. After the
first message, the same binary supports paginated turns and items. Other
unsupported-method errors remain visible rather than being treated as empty
history or writer conflicts.

Basic initialization, `--stdio`, existing thread/turn methods, and the three
mobile permission modes remain compatible. Optional APIs for new product features
are not automatically enabled by this compatibility update.
