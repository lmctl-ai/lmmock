# ACP provider profiles

One annotated lmcode program implements six profiles. The frozen P0 manifests
are fixtures/providers/acp-*.json; model families are data, not separate handlers.
The configurations here are deterministic synthetic values, derived from ACP v1
and lmctl's pinned profiles, not captured vendor/account traffic.

## Scope and observed client behavior

The tested client is privately installed npm lmctl/lmscript 0.2.448. Read-only
source cross-check: mono 8e334dda, transports/acp.ts, parsers/acp.ts,
transports/acp-profiles and failure-class.ts. Public references retrieved
2026-10-31: https://agentclientprotocol.com/protocol/v1/initialization,
https://agentclientprotocol.com/protocol/v1/session-setup and
https://agentclientprotocol.com/protocol/v1/prompt-turn.

| Profile | Configuration boundary | Qualification |
| --- | --- | --- |
| Copilot | --acp --yolo; model selection warns and is ignored | Actual published adapter |
| Kimi | mode=auto before model via set_config_option; model rejection warns | Actual published adapter |
| OpenCode | model then effort via set_config_option; permission=allow file installed | Actual published adapter |
| Hermes | HERMES_YOLO_MODE=1; set_mode then encoded provider:model; fail-closed | Actual published adapter |
| Miniplayer | acp -m provider/model | Actual published adapter |
| Gemini | --acp -y --skip-trust --model | Wire profile plus actual adapter-disabled refusal; no live adapter claim |

The 41 cases cover initialize/auth refusal, new/load, replay isolation,
configuration ordering, thoughts/tool call/tool result/text updates, server
permission-request rejection, active session/cancel and subsequent reuse,
missing final response, stale load fallback and configuration errors. One-shot
lm_prompt falls back visibly to session/new after a load refusal. This is not
claimed as strict bundle-resume qualification. Kimi/OpenCode's nonfatal model
warnings remain distinct from Kimi/Hermes's fatal permission-mode errors and
Hermes's fatal model errors.

The native adapter auto-replies -32601 to session/request_permission; the fixture
records a failed synthetic tool, then returns explicit text. It never runs a tool
or reads provider credentials. Reply text must exclude session-load replay,
thoughts and tool content. ACP usage_update metadata is not billing evidence:
API and DB token facts stay null, with native_record_missing/session_totals_only
on successful turns. No fabricated token count or cost is accepted.

## Execution

After the normal build, run the profile matrix:

```sh
node tests/acp-profiles/run.mjs
node tests/acp-profiles/controls.mjs
```

Or serve one explicitly configured profile over the existing stdio protocol:

```sh
lmmock stdio acp --lmcode build/acp-profiles.dslb --scenario fixtures/acp/kimi-success.json --alias Worker -- acp
```

The trailing argv is the fake provider's actual process argv and is checked by
lmcode. Published-client tests use a portable test-only native launcher copied to
an owned fake bin directory. It only forwards pipes and records protocol/argv;
profile policy is not in Go. HOME, provider homes, cache/config/data paths and the
SQLite database are private. Stdout/stderr, exit, configuration files, session ID,
requested model, transcript response and token facts are asserted.

## Cancellation and protocol lifecycle

The previous stdio host demanded immediate replies. Generic defer_reply/reply_to
now retains an inbound ID while returning control to the input loop. ACP stores
that receipt in lmcode, resolves it only on session/cancel, emits cancelled once,
and accepts the next turn. No provider method name exists in the Go continuation
code. Duplicate/unknown receipts, premature EOF, duplicate pending IDs and deferred
notifications are refused. Reusing a receipt is an error.

All five native platforms run event-driven RPC cancellation. A separate Linux
published-client check sends SIGTERM only after an observed partial ACP update and
asserts exit 143, no successful reply and preserved private session/token facts.
It has no sleep or per-case deadline. Native signal conventions on Windows/macOS
are not claimed by that Linux check.

## Controls and limits

Compiled handler controls disable auth refusal for each profile, reverse chunks,
complete a deliberately missing terminal, and change cancelled to end_turn.
They must fail assertions through real clients or the explicitly labeled Gemini
wire profile. Changing expected error_class also fails. A production Go overlay
that discards deferral fails its lifecycle test. Compiler/load errors are not catches.

Auth errors map to auth_required. Missing terminal errors map to the published
client's conservative unknown, not an invented protocol_error. The parser does not
supply a special invalid-order/usage class; reversed chunks are caught by exact
reply checks, and usage metadata is explicitly asserted unavailable. Model-family
rows are synthetic selectors; one representative per profile is exercised, not a
claim of live availability for a catalog namespace. No OAuth server, native vendor
session databases, model discovery endpoint or paid model is simulated.
