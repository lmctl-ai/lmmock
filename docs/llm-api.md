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
  @ai-sdk/openai-compatible 3.0.81; exact npm lockfile in tests/vercel.
  Thirty scenarios cover three provider paths, text, SSE, usage, tool execution
  and streamed multi-step workflows, tool-choice none, structured output,
  embeddings, model discovery, HTTP errors, malformed JSON, retries, disconnects,
  deadlines and credential rejection.
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
Native Anthropic/Gemini/Bedrock formats, audio/images/files and Responses
persistence are not implemented. Vercel compatibility means the pinned versions
and scenarios above, not every provider or feature in the SDK.
