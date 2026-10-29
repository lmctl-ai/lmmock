# Adding n8n HTTP services with lmcode

Updated 2026-10-29. Read this before adding another service mock.

## Architectural intent

The generic Go HTTP server supplies transport, lifecycle, state and Exchange.
Service behavior belongs in lmcode scripts. Annotated functions implement the
Handler interface; top-level route calls bind HTTP methods and paths to those
annotations. Do not add a Go dispatcher or separate Node/Python HTTP server for
each new service.

A new HTTP service normally needs examples/<service>.lms and its
<service>-client.json suite, plus test registration and documentation. Change
Go/schema only for a reusable host capability that does not already exist.
The non-HTTP mocks under internal/proto are separate; Handler does not implement
their wire protocols.

## Minimal working Handler

Place this beside examples/schema.json, or adjust the schema path:

~~~text
p = _lmcode_ schema="schema.json";

@read_widget
fn read_widget(x: Exchange) -> void implements Handler {
 let c = json_parse(config());
 let token = "fixture-token";
 if (c.has("token")) { token = c.get("token").as_string(); }

 if (!secure_equal(x.header("Authorization"), "Bearer " + token)) {
  x.status(401);
  x.json("{\"error\":\"unauthorized\"}");
 } else {
  x.json("{\"id\":" + json_quote(x.param("id")) + ",\"name\":\"Fixture\"}");
 }
}

route("GET", "/v1/widgets/{id}", "read_widget");
end _lmcode_
savedsl(p, ARGV[0]);
~~~

The annotation is an identifier, not a URL. The third route argument must match
it exactly. The required signature is one Exchange argument and void return,
with implements Handler. Ordinary helper functions need no annotation/interface.
Use whole-segment path wildcards, such as {id}; a pattern such as bot{token} is
not supported by Go ServeMux. See telegram.lms for splitting that path segment.

Top-level lmcode runs once at startup to register routes. The host checks
interface signatures, annotations and route conflicts before opening a listener.
Each request invokes the selected handler with a fresh VM registry. Use Exchange
state methods to retain data across requests, not local variables.

## Exchange and language rules

- Requests: method(), path(), param(name), query(name), header(name), body(),
  request_json(). Read the embedded schema for exact types.
- Responses: status(code), set_header(name,value), json(JSON-text), send(text).
  json takes serialized JSON text. Use json_quote for dynamic strings and
  Json.to_json() for nested values; never concatenate unescaped request text.
- State: get/set store strings; increment is atomic; take atomically reads and
  deletes. A sequence of get then set is not an atomic update. Prefix state keys
  by resource type. Store a created response and retrieve it by captured ID.
- Configuration: config() returns the current --config JSON document. Use
  synthetic defaults and allow token overrides. Compare secrets with secure_equal.
- Helpers include form_get, url_encode/decode, basic_id/secret, json_quote,
  random_token, sha256_url and now_ms. Match the vendor's auth/body encoding.
- Faults: delay(ms), disconnect(), status and response bodies. Use deterministic
  triggers that allow healthy and failing requests in one suite. The six newest
  presets use X-Lmmock-Fault: 429 or 500 after authentication.
- This is lmcode, not JavaScript: no ternary operator; no early/nested returns.
  Value helpers accumulate a result and return at the end. Handler branches use
  if/else, or while(true) with break for a single-pass early-exit pattern.
- Keep internal/mock/schema.json and examples/schema.json byte-identical. Schema
  changes require recompiling fixtures and checking the pinned host version.
- Scripts are trusted. VM execution has no instruction deadline. Avoid unbounded
  loops. Responses, including the current LLM SSE fixture, are buffered.

## Inspect n8n before implementing

Use ~/repos/foss/n8n/packages/nodes-base as the local source. Record its commit
and the files inspected. Current expanded presets used
9f84b4b9f7b2744bb8df728c0caa6ed16a77fd2b.

1. Read credentials/<Service>*.credentials.ts for auth and the credential-test
   request. Implement that endpoint, not just a convenient identity endpoint.
2. Read the selected node version's GenericFunctions.ts, transport/, helpers,
   actions and tests. Identify base path, methods, body encoding, response
   envelope, pagination fields/headers and termination conditions.
3. Choose a useful operation subset: credential check, list/search, create,
   read-back. Add update/delete only when implemented and tested.
4. Add missing/wrong auth, invalid arguments, missing resources, rate limits and
   server errors. Match HTTP status and vendor error structure. An HTTP 200 may
   still carry a vendor error, as Slack does.
5. Exercise pagination using returned cursors. Ensure the final page terminates
   the client's loop. Asana uses next_page.uri; Trello fetches until an empty page.
   Pagination URLs must point to the loopback mock, never the live vendor.
6. Document fixed resource IDs and limitations. Our six newest list/search
   responses are fixed snapshots; newly created resources are available by ID
   but are not merged into those lists.

Useful templates: notion.lms (version header, body cursor), airtable.lms (query
offset), gitlab.lms (Private-Token, X-Next-Page), asana.lms (data wrapper and
next_page), trello.lms (key/token query and path token). See
[n8n auth shapes](n8n-connector-auth-shapes.md) for the older presets.

## Add and run the contract suite

Each suite is {"steps":[...]}. Steps support name, method, path, headers, body,
status (default 200), json dot-path assertions, contains, and capture.
Capture an ID/cursor with {"id":"id"} or {"cursor":"next_cursor"}; subsequent
steps interpolate it with ${id} or ${cursor}. Assert read-after-create,
both pages, final-page termination and error bodies as well as status.

The native client also supports timeoutMs and networkError. The current lmconn
service-suite adapter does not implement every native option: extend that
adapter when introducing network-error/timeout-specific suite steps.

Register the name in both:

- scripts/compile-examples.sh (source compilation)
- tests/protocols.mjs (native exec + check suite)

Run from lmmock's root, with Go and lmmock on PATH. Replace <service> with the
actual service name:

~~~sh
npm i -g @lmctl-ai/lmctl
lmmock exec --script examples/<service>.lms -- lmmock check --suite examples/<service>-client.json
lmscript --no-preflight test.lms
~~~

npm installs both lmctl and lmscript. lmctl test.lms is another supported entry.
test.lms compiles examples and negative fixtures, runs Go tests, builds the host,
and runs real-process/native-client tests. No Python test driver is needed.
The exec command exports LMMOCK_URL and closes the loopback server on client exit.

## Propagate to lmconn and Actions

The checkout is ~/repos/lmctl-ai/lmconnectors; the public repo/executable is lmconn.
Read its AGENTS.md. Its branch is master; lmmock uses main.

1. Commit the reviewed scripts and suites in lmmock first. The sync tool copies
   committed git objects, not working-tree edits.
2. In lmconn, add the service name to scripts/sync-lmmock.mjs and
   test/service-mocks.test.js.
3. Run node scripts/sync-lmmock.mjs ../lmmock. It vendors source/schema/suites and
   records the source commit and SHA-256 hashes in test/mocks/lmmock.lock.json.
4. Run with the local host:
   LMMOCK_BIN=/absolute/path/to/lmmock/build/lmmock lmscript --no-preflight test.lms
5. Keep Actions' npm install -g @lmctl-ai/lmctl step before the lmscript runner.
   It downloads a pinned, checksum-verified lmmock binary. Bump that pin if host
   features change; script-only additions can use a compatible older host.
6. Update coverage docs and this index. Pull --rebase before pushing each repo.
   Verify Linux/macOS/Windows tests and release workflows for code changes.

Native lmmock suites test the fixtures. lmconn suites run the actual HTTP CLI
against those same fixtures and check its envelope and response body. Neither
is proof that every operation in the n8n node or the live vendor API works.
Say "selected service contracts", not "all n8n connectors covered".

## Current baseline

lmmock v0.1.15 includes the six new presets: Notion, Airtable, HubSpot, GitLab,
Asana and Trello. Together with Slack, Discord, Telegram, Jira and Stripe,
11 shared service suites contain 131 checks. GitHub, OAuth and LLM have separate
protocol/adversarial tests. lmconn had 214 passing tests on all three OS families
after the expansion. Counts and release pins are a snapshot, not fixed targets.
See [supported routes and limits](../docs/http-presets.md).


Wave 9 adds PagerDuty, Datadog and Linear selected contracts; see the coverage
section in docs/http-presets.md for routes and source provenance. Note that
Datadog is credential-only in the inspected n8n checkout, and Linear API keys
use raw Authorization while OAuth uses Bearer. Do not infer auth from a hint.

Wave 9 additions: `bitbucket`, `circleci`, `paypal` are registered in both runners.
See docs/http-presets.md for exact source revisions, credential overrides and
limits. CircleCI's base includes `/api`; PayPal's token request is form encoded,
not JSON. Bitbucket's Basic credential test lives in the trigger node rather
than the credential class. CircleCI/PayPal credential classes have no test in
the inspected checkout; their documented identity probes supplement operations.
