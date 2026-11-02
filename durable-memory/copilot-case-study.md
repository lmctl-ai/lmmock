# Case Study: GitHub Copilot CLI Mock & Integration Testing in lmmock

This document records the architectural design, wire protocol contracts, and test integration for mocking the official `@github/copilot` CLI within **lmmock**.

---

## 1. Overview & Architecture

GitHub Copilot CLI (`@github/copilot@1.0.105`) supports multiple wire protocols and backend providers:
1. **OpenAI Responses Streaming API** (`COPILOT_PROVIDER_WIRE_API=responses`): The primary protocol used by modern Copilot agents for reasoning, conversational response, and tool invocation.
2. **OpenAI Chat Completions API** (`COPILOT_PROVIDER_WIRE_API=completions`): Standard legacy streaming chat chunks.
3. **Anthropic Messages API** (`COPILOT_PROVIDER_TYPE=anthropic`): Anthropic streaming messages format (`message_start`, `content_block_delta`, etc.).
4. **Tool Execution Flow**: Structured assistant tool calling triggering local shell/bash execution.
5. **Model Catalog & Discovery**: Platform endpoints (`/models`, `/auto`) queried prior to model execution.

The mock implementation is authored in lmcode DSL at [`examples/copilot.lms`](file:///home/ubuntu/repos/lmctl-ai/lmmock/examples/copilot.lms) and compiled to standalone bytecode `build/copilot.lmcode`.

---

## 2. Wire Protocol Contracts & Nuances

### A. OpenAI Streaming Responses Protocol (`POST /v1/responses`, `POST /responses`)
Unlike standard chat completions, Copilot's internal Rust agent parser requires a strictly ordered sequence of SSE events. Omitting events or transmitting out-of-order sequence numbers causes the client to abort with `Responses stream ended without a completed response`.

The exact required event order implemented in `examples/copilot.lms`:
1. `event: response.created`
2. `event: response.in_progress`
3. `event: response.output_item.added` (`type: "message"`)
4. `event: response.content_part.added` (`type: "text"`)
5. `event: response.output_text.delta` (`delta: "..."`)
6. `event: response.output_text.done` (`text: "..."`)
7. `event: response.content_part.done`
8. `event: response.output_item.done`
9. `event: response.completed`

**Mandatory Fields:**
- `sequence_number`: Monotonically increasing integer starting at 1 on every event.
- `type`: Must match the event name (e.g., `{"type": "response.output_text.delta", ...}`).

### B. Tool Calling Execution Protocol
When `mode` is set to `"tool"` (via [`examples/copilot-config-tool.json`](file:///home/ubuntu/repos/lmctl-ai/lmmock/examples/copilot-config-tool.json)), the mock sends a `function_call` output item:
- `event: response.output_item.added` with `item.type = "function_call"` and `item.name = "bash"`.
- `arguments`: `{"command": "echo tool-executed"}`.
- Copilot CLI executes the command and transmits the tool output back in the subsequent round.

### C. Model Discovery Contract (`GET /models`, `GET /v1/models`)
The Copilot CLI validates available models against the catalog before running prompts. The mock responds with:
- Models: `gpt-5.3-codex`, `gpt-4o`, `claude-sonnet-4`.
- Attributes: `is_chat_default: true`, `is_chat_fallback: true`, `model_picker_enabled: true`, `supported_endpoints: ["/responses", "/chat/completions", "/v1/messages"]`.

---

## 3. Test Suites & Verification

### A. Declarative Assertion Suite (`examples/copilot-client.json`)
A 7-step HTTP assertion suite executed with `lmmock check`:
```bash
lmmock check -url http://127.0.0.1:19095 -suite examples/copilot-client.json
```
Validates `/models`, `/auto`, `/v1/chat/completions`, `/v1/responses`, `/v1/messages`, and `/copilot_internal/*` endpoints.

### B. Live Integration Suite (`tests/copilot.mjs`)
A Node.js test driver that spawns the real `@github/copilot` executable against `lmmock` and verifies:
1. Standard OpenAI Chat Completions provider (`COPILOT_PROVIDER_WIRE_API=completions`).
2. OpenAI Streaming Responses provider (`COPILOT_PROVIDER_WIRE_API=responses`).
3. Anthropic Messages provider (`COPILOT_PROVIDER_TYPE=anthropic`).
4. Automated Tool Execution round-trip (`echo tool-executed`).

Ran via `lmmock exec`:
```bash
lmmock exec --lmcode build/copilot.lmcode --config examples/copilot-config.json -- node tests/copilot.mjs
```

---

## 4. GitHub Actions CI Matrix

The Copilot CLI integration test is registered in [`.github/workflows/release.yml`](file:///home/ubuntu/repos/lmctl-ai/lmmock/.github/workflows/release.yml):
```yaml
- name: GitHub Copilot CLI integration test
  run: |
    npm install -g @github/copilot
    ./build/lmmock exec --lmcode build/copilot.lmcode --config examples/copilot-config.json -- node tests/copilot.mjs
```

Executed across all 5 runner targets:
- `ubuntu-24.04` (linux-x64)
- `ubuntu-24.04-arm` (linux-arm64)
- `macos-15` (macos-arm64)
- `macos-15-intel` (macos-x64)
- `windows-2022` (windows-x64)

All 5 matrix jobs run headless, offline (`COPILOT_OFFLINE=true`), with zero external API dependencies or secrets.
