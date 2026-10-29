# Closing the n8n coverage gap: what lmmock still needs

Snapshot: 2026-10-29. Companion to [n8n-connectors-uncovered.md](../n8n-connectors-uncovered.md)
(which inventories the 222 uncovered HTTP families). This document answers a different question:
beyond per-service HTTP presets, what *capabilities* must lmmock grow so that **every** n8n
connector category becomes testable offline? Items are ordered by leverage. Status: not started;
revisit as waves after the HTTP-preset expansion.

## Already covered (no work needed)

- HTTP REST APIs: scripted presets (`examples/<service>.lms` + contract suites).
- Wire protocols: redis, mqtt, smtp, imap, mongodb, postgres, kafka, amqp (`lmmock proto`).
- OAuth2 authorization-code + PKCE: `examples/oauth.lms` (validated against real n8n).
- LLM provider HTTP shapes: `examples/llm.lms` (OpenAI-compatible family).
- Utility/plumbing n8n nodes (Code, DateTime, RSS fetch, generic HTTP Request): no mock needed.

## Gap 1 — MySQL wire protocol (highest leverage)

n8n ships MySQL and MariaDB nodes; neither is testable without a server.
**Approach**: embed `github.com/dolthub/go-mysql-server` (Apache-2.0) — a complete
MySQL-compatible server engine in Go — behind the `internal/proto` contract.
Watch: its Go-version requirement and dependency tree size; verify the *real* MySQL
Connector/J (lmclient's JDBC bundle) against it. Effort: medium; library does the heavy lifting.

## Gap 2 — Postgres-wire family profiles

CockroachDB, TimescaleDB, QuestDB, and CrateDB n8n nodes all speak the postgres wire protocol.
Our `internal/proto/postgres` mock may satisfy them with per-family fixture profiles
(different `version()` strings, catalog probes, type quirks). **Approach**: config knob
`{"flavor": "cockroachdb"|"timescaledb"|...}` on the existing mock; verify against each n8n
node's actual startup queries. Effort: small per family.

## Gap 3 — WebSocket mock

n8n has a WebSocket node; the HTTP engine explicitly serves HTTP/1.x only (no upgrade handling).
**Approach**: either an `internal/proto/websocket` package (canned accept/echo/scripted frames)
or upgrade support in the HTTP engine with lmcode handlers for `onMessage`. The proto package is
simpler and matches the framework. Effort: small-medium.

## Gap 4 — SSH / SFTP / FTP mocks

n8n SSH and FTP nodes. Also feeds lmssh (lmctl-ai/lmssh) testing.
**Approach**: `golang.org/x/crypto/ssh` server side for SSH/SFTP;
`github.com/fclairamb/ftpserverlib` (MIT) for FTP. Both as `internal/proto` packages with
canned filesystems and `users` config. Effort: medium each; SFTP subsystem is the fiddly part.

## Gap 5 — OAuth 1.0a verifying mock

X/Twitter, Discourse, and Trello's legacy flow use OAuth 1.0a. lmconn's coverage doc
(lmmock `docs/lmconn-coverage.md`) lists OAuth 1.0a signing as absent.
**Approach**: an `examples/oauth1.lms` preset that *verifies* HMAC-SHA1/PLAINTEXT signatures
(request token, authorize, access token legs), not merely accepts tokens — client bugs must fail.
Effort: medium (signature base-string correctness is the whole game).

## Gap 6 — Webhook trigger dispatcher

n8n trigger nodes *receive* webhooks; testing them needs lmmock as the event **source**.
**Approach**: a small mode that POSTs canned/signable payloads (JSON + optional HMAC signature
headers, matching how vendors sign webhooks) at a client-supplied URL on a schedule or on
request. Could be an lmcode host capability (`webhook(target, body, headers)`) or a proto.
Effort: small. Unblocks trigger-node testing for every preset we already ship.

## Gap 7 — HTTP-API databases as presets

ClickHouse (HTTP interface), InfluxDB (write/query over HTTP), Elasticsearch/OpenSearch,
Snowflake (SQL REST API with session-token flow). No new protocol work — wave-9-style presets,
though Snowflake's login/token exchange needs a stateful fixture. Effort: small each.

## Holds (documented, not planned)

| Family | Reason |
| --- | --- |
| MS SQL Server (TDS) | [feasibility-sqlserver.md](feasibility-sqlserver.md): PRELOGIN/LOGIN7/TLS-in-TDS + JDBC metadata RPCs, 2–4 weeks hand-rolled |
| Oracle (TNS) | Proprietary wire protocol, same class as TDS, larger |
| Cassandra/ScyllaDB (CQL) | Binary CQL v4 framing + schema agreement; minimal subset possible but low demand |
| AWS / GCP / Azure | Excluded by operator directive; lmaws covers the client side |
| SAP/OData enterprise | License-access for verification, not protocol difficulty |
| n8n LangChain/AI nodes (127 registrations) | Excluded: we use lmctl's provider CLIs and the llm preset instead |

## Suggested wave order when revisited

1. MySQL (Gap 1) — most n8n nodes unblocked per effort
2. Webhook dispatcher (Gap 6) — multiplies the value of every existing preset
3. OAuth 1.0a (Gap 5) — named gap in lmconn coverage
4. SSH/SFTP/FTP (Gap 4) — pairs with lmssh
5. WebSocket (Gap 3)
6. Postgres-wire flavors (Gap 2) and HTTP-API databases (Gap 7) — opportunistic
