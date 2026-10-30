# Scriptable LLM API mock and real-client acceptance

The server is examples/llm-api.lms. Annotated lmcode Handler functions
implement the API; the generic Go host provides loopback HTTP and lifecycle.
No Python server or provider-specific Go handler is required.

This extends the older examples/llm.lms fixture without changing its deliberate
credential-echo behavior used by connector redaction tests. The new fixture does
not echo credentials in authentication errors.

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
| GET /__inspect | Last request for assertions; synthetic test credentials only |

Unprefixed /chat/completions, /responses, /embeddings and /models also work.
Chat additionally accepts /api/v1/chat/completions.

## Run it

Install the compiler and start a server:

~~~sh
npm i -g @lmctl-ai/lmctl
lmmock serve --script examples/llm-api.lms --listen 127.0.0.1:4000
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

When apiKey is configured, OpenAI requires Authorization: Bearer, Anthropic
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
  "structuredOutput": {"city": "Paris"},
  "mode": ""
}
~~~

An omitted apiKey permits requests without authentication. The acceptance tests
set it explicitly. Default text is pong; default structured output is
{"city":"Paris"}. The fixture does not synthesize values from JSON Schema.

Modes unauthorized, rate-limit, server-error, bad-json, timeout and disconnect
exercise SDK failure handling. Chat mode retry fails its first request with 429
and then succeeds. Timeout delays the response by one second. Chat also retains
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
Client deadlines during a delayed handler are tested separately.

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
lmscript --no-preflight test.lms
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
