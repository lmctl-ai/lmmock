# Scriptable LLM API mock and real-client acceptance

The server is examples/llm-api.lms. Annotated lmcode Handler functions
implement the API; the generic Go host provides loopback HTTP and lifecycle.
No Python server or provider-specific Go handler is required.

This extends the older examples/llm.lms fixture without changing its deliberate
credential-echo behavior used by connector redaction tests. The new fixture does
not echo credentials in authentication errors.

Response templates are now native lmscript JSON objects, passed into lmcode at
script execution. Handlers use standard `template_render` with explicit JSON string encoding
and immutable structured updates. See [runtime JSON data flow](runtime-json.md).

## Implemented interfaces

| Route | Behavior |
| --- | --- |
| POST /v1/chat/completions | Text, function tool calls and tool-result continuation, JSON output, SSE and usage |
| POST /v1/responses | Text, function calls and continuation, JSON output, Responses SSE events and usage |
| POST /v1/embeddings | Single/batch inputs return deterministic three-dimensional vectors |
| GET /v1/models | Deterministic model discovery |
| POST /v1/messages | Native Anthropic Messages: text, tool_use/tool_result, structured output and named SSE events |
| POST /v1beta/models/{model}:generateContent | Native Gemini text, functionCall/functionResponse and structured output |
| POST /v1beta/models/{model}:streamGenerateContent | Native Gemini SSE with usageMetadata |
| POST /v1beta/models/{model}:embedContent | Gemini single embedding |
| POST /v1beta/models/{model}:batchEmbedContents | Gemini batch embeddings |
| POST /api/chat | Ollama native JSON or NDJSON chat, function tool calls and tool-result history |
| GET /api/tags | Ollama local-model list for the mock model |
| GET /__inspect | Last request for assertions; synthetic test credentials only |
| GET /__requests | `{"count":N}`: LLM requests served so far (chat, responses, embeddings, Anthropic, Gemini); lets a client prove that a retry-mode 429 attempt happened |

Unprefixed /chat/completions, /responses, /embeddings and /models also work.
Chat additionally accepts /api/v1/chat/completions.

## Run it

Without Node or the compiler, use the program embedded in the lmmock binary:

~~~sh
lmmock serve --builtin llm-api --listen 127.0.0.1:4000
lmmock exec --builtin llm-api --config settings.json -- COMMAND
~~~

`--builtin llm-api` is exclusive with `--lmcode`. The bytecode in
cmd/lmmock/builtin/llm-api.lmcode is generated from examples/llm-api.lms by
scripts/compile-builtin.sh (CI checks it is current). Alternatively, install the
compiler and start a server from source:

~~~sh
npm i -g @lmctl-ai/lmctl
lmmock serve --lmcode "$PRIVATE_PROGRAM_FILE" --listen 127.0.0.1:4000
~~~

Configure a normal Vercel AI SDK provider with the local base URL:

~~~js
import { createOpenAI } from '@ai-sdk/openai';
import { generateText, streamText } from 'ai';

const openai = createOpenAI({
  baseURL: 'http://127.0.0.1:4000/v1',
  apiKey: 'fixture-key',
});
const result = await generateText({
  model: openai('mock-agent'), // default Responses API
  prompt: 'ping',
});
console.log(result.text); // pong
const streamed = streamText({
  model: openai.chat('mock-agent'), // Chat Completions API
  prompt: 'ping',
});
for await (const text of streamed.textStream) process.stdout.write(text);
~~~

For ephemeral integration tests, use lmmock exec. It exports LMMOCK_URL to the
child, waits for the client to finish, propagates its exit code and closes the
server. See tests/vercel/run.mjs for a complete temporary-config example.

## Anthropic and Gemini clients

All three API families run on the same server. Use native SDK providers:

~~~js
import { createAnthropic } from '@ai-sdk/anthropic';
import { createGoogleGenerativeAI } from '@ai-sdk/google';
import { generateText } from 'ai';

const anthropic = createAnthropic({
  baseURL: 'http://127.0.0.1:4000/v1',
  apiKey: 'fixture-key',
});
const gemini = createGoogleGenerativeAI({
  baseURL: 'http://127.0.0.1:4000/v1beta',
  apiKey: 'fixture-key',
});
await generateText({ model: anthropic('claude-sonnet-4-5'), prompt: 'ping' });
await generateText({ model: gemini('gemini-2.5-flash'), prompt: 'ping' });
~~~

These model IDs select the clients' request formats; no live models are called.
The mock accepts arbitrary model IDs for these native routes.

The Ollama routes are local protocol fixtures, not an inference backend. `/api/chat`
accepts documented function tool definitions and returns the first offered function
with synthetic `city: Paris` arguments. A later `role: tool` message with `tool_name`
returns a deterministic tool-result summary. Streaming responses use one JSON object
per NDJSON line and finish with a `done: true` usage record. `/api/tags` lists the
configured mock model. Auth, rate-limit, server, missing-model and unknown-model
responses are deterministic and use synthetic data; no Ollama server or credentials
are required. See [the Ollama API reference](https://github.com/ollama/ollama/blob/main/docs/api.md).

When apiKey is configured, OpenAI requires Authorization: Bearer, Anthropic requires x-api-key (set `"anthropicBearer": true` to also accept Authorization: Bearer, as Claude Code's ANTHROPIC_AUTH_TOKEN sends)
requires x-api-key, and Gemini requires x-goog-api-key or a key query parameter.
Anthropic also requires an anthropic-version header; its SDK supplies it.
A Bearer token alone is rejected on the native Anthropic and Gemini endpoints.

Native errors retain their provider envelope. Anthropic uses type:error and
error.type (401 authentication_error, 429 rate_limit_error, 529 overloaded_error).
Gemini uses error.code/status/message (403 PERMISSION_DENIED,
429 RESOURCE_EXHAUSTED, 503 UNAVAILABLE). Unsupported Gemini operation suffixes
return 404 NOT_FOUND. The same configured failure modes apply across providers.

Wire formats were checked against the installed native provider source and
the [Anthropic SDK provider documentation](https://ai-sdk.dev/providers/ai-sdk-providers/anthropic)
and [Gemini API reference](https://ai.google.dev/api/generate-content).
Vercel is a sample acceptance client, not the definition of the server's scope.

## Customize with lmcode or configuration

Pass --config settings.json. Available common fields:

~~~json
{
  "apiKey": "fixture-key",
  "text": "custom reply",
  "replies": [{"contains": "What is the total", "text": "10"}],
  "structuredOutput": {"city": "Paris"},
  "mode": "",
  "toolCalls": true
}
~~~

`replies` scripts a conversation: `[{"contains": "NEEDLE", "text": "REPLY"}, ...]`.
Each rule's needle is searched in the raw JSON request body; the rule whose needle
occurs latest in the body wins (clients resend their history with the newest user
turn last, so keying each rule on a turn's wording answers each turn differently),
ties keep the first listed rule, and a request matching no rule falls back to `text`
and then to `pong`. A rule's optional `delayMs` (0-60000) holds that response, so a
client can be observed while its turn is still in flight. It applies to chat,
responses, Anthropic, Gemini and Ollama chat.
A three-turn memory check looks like this (tests/replies.mjs exercises every route):

~~~json
{"toolCalls": false, "replies": [
  {"contains": "first number is 3", "text": "Noted."},
  {"contains": "second number is 7", "text": "Noted."},
  {"contains": "What is the total", "text": "10"}]}
~~~

`toolCalls` defaults to true. Set `"toolCalls": false` for clients that always
declare `tools` (coding CLIs, agents) but only need a plain reply: on every
provider route the mock then answers with the text (`pong` by default) instead of
calling the first offered tool. Tool-result continuations are unaffected.

An omitted apiKey permits requests without authentication. The acceptance tests
set it explicitly. Default text is pong; default structured output is
{"city":"Paris"}. The fixture does not synthesize values from JSON Schema.

Modes unauthorized, rate-limit, server-error, bad-json and disconnect
exercise SDK failure handling. Chat mode retry fails its first request with 429
and then succeeds. The former timeout mode returns HTTP 400; timing modes are unsupported. Chat also retains
the legacy unknown-model and overloaded model test cases. Rate limits include
Retry-After. Configuration is read per request.

For other tool schemas, edit the annotated handler's canned tool arguments.
The current example chooses the first offered function with {"city":"Paris"},
honors tool_choice none, and answers with the tool result on the next turn.
It is a deterministic protocol fixture, not a model. It does not implement
arbitrary tool selection, parallel tools, hosted tools or semantic reasoning.

SSE messages are sent as one buffered HTTP response. streamText parsing,
function-call assembly, usage and final events are tested; token timing,
backpressure and cancellation during incremental token delivery are not.
Caller-owned cancellation is tested with an already-aborted signal; the mock adds no delay.

## Acceptance tests

The tests use real installed client libraries and a real loopback server.
No fetch stub, custom Vercel provider or live LLM credentials are used.

- Vercel AI SDK: ai 7.0.157, @ai-sdk/openai 4.0.105,
  @ai-sdk/openai-compatible 3.0.81, @ai-sdk/anthropic 4.0.92 and
  @ai-sdk/google 4.0.108; exact npm lockfile in tests/vercel.
  Sixty scenarios cover five provider paths, text, SSE, usage, tool execution
  and streamed multi-step workflows, tool-choice none, structured output,
  embeddings, model discovery, HTTP errors, malformed JSON, retries, disconnects,
  deadlines and credential rejection, including native authentication and errors.

Run the normal suite after installing the test-only npm packages:

~~~sh
npm ci --prefix tests/vercel
lmscript  test.lms
~~~

test.lms drives the real Vercel client on all five release platforms. The mock
server has no LiteLLM dependency or LiteLLM compatibility requirement. LiteLLM
was a design reference only; Vercel AI SDK acceptance is the test target.

## Scope

This is a standalone mock LLM API server. It does not route requests to models,
perform inference, or implement production billing or provider translation.
OpenAI, Anthropic and Gemini text/tool contracts are supported. Bedrock,
multimodal generation, provider-hosted tools, Anthropic thinking, Gemini grounding,
provider model catalogs (except the OpenAI fixture) and Responses persistence are
not implemented. Vercel compatibility means the pinned versions
and scenarios above, not every provider or feature in the SDK.

Embedded executions emit the same invocation-result envelope as file-backed executions.
For a custom protocol launcher, the builtin's virtual registration is the absolute
path `builtin/llm-api.lmcode` resolved against the host working directory; no file at
that path is read or created. Invocation metadata must register exactly that path.
A dry invocation validates the embedded program without reading config, opening a
listener, writing audit/readiness files, or starting a client. A normal
`run_lmcode` launcher should use `--lmcode $DSLFILE` to execute its supplied program.
