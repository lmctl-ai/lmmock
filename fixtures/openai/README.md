# OpenAI Chat and Responses fixture family

The annotated Handler implementations in handler.lms own two POST routes:
/v1/chat/completions and /v1/responses. Go supplies only the existing HTTP and
stream_event transport. Model selectors, payloads and faults are wire.json data;
fixtures/providers/openai-*-native.json follow the shared P0 manifest schema.
These are synthetic representatives, not captures or a claim to support every
vendor/model. Both family selectors share handlers. Unknown models return 404.

Credentials are intentionally distinct synthetic bearer keys per route. Wrong,
missing and swapped keys return 401. The config selected field chooses a case;
X-Lmmock-Case overrides it for the shared manifest runner. Unknown case names
return 400. Never send these fixture headers or keys to a vendor.

Cases: success, tools, bad-usage, auth-error, rate-limit, server-error,
bad-terminal and missing-terminal. stream=true chooses ordered SSE events;
otherwise the handler sends JSON. Chat uses [DONE]; Responses uses typed events,
sequence_number and response.completed. Tools are function-call items with
JSON arguments. Synthetic usage is input 10 (cached 3), output 2 (reasoning 1),
total 12. A 429 carries retry-after 0, remaining-requests 0 and request ID.
No sleeps, mock deadlines or retry loops are implemented.

## Tests and boundary

Compile with private npm lmscript 0.2.448:

    lmscript --no-preflight fixtures/openai/handler.lms build/openai.dslb
    node tests/openai/run.mjs build/lmmock
    node tests/openai/controls.mjs build/lmmock

The normal test.lms invokes these tests on the five native OS jobs. SDK packages
come from the existing locked tests/vercel package. Tests assert JSON/SSE text,
token facts, tool-call arguments, HTTP failures/headers and malformed terminal
usage. JSON invalid usage rejects as AI_APICallError; event invalid usage reports
AI_TypeValidationError. Compiled handler mutations cross the two route bindings;
the independent wire oracle must reject both, after a green baseline.

The dedicated openai-http Actions job additionally downloads published lmctl
0.2.448 and checks out public Miniplayer eb4089de6337344e0a758f0a0d41448b71b610e2
with its lockfile. It drives the real lm_agent -> ACP -> Miniplayer -> HTTP path,
using a private home/database/catalog, a guarded local executable shim, synthetic
keys and loopback routes. Both dialects verify stdout result, process exit,
progress stderr, one session per turn, stored token details and failed sessions.
Malformed usage returns an ACP error and lmctl telemetry error_class Error with
reason provider_no_result; this pin does not expose a dedicated UsageError.
Do not confuse the SDK exception name with lmctl's provider classification.

Miniplayer's pinned lmctl integration uses generateText JSON, not event streaming.
SSE is tested with the actual SDK parser separately, not represented as an lmctl
streaming integration. The missing-terminal fixture exposes an omission for
client tests; no guarantee of SDK EOF/order enforcement is claimed. Tool tests
validate serialized call items, not execution of native shell/file tools.
No production daemon or provider credentials are needed.

Protocol sources (read 2026-10-31):
- https://developers.openai.com/api/reference/resources/chat/subresources/completions/streaming-events
- https://developers.openai.com/api/reference/resources/responses/streaming-events
- public Miniplayer src/providers.js and src/acp.js at the pinned revision.
