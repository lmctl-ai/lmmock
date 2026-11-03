# Jira/Confluence MCP and Xray selected contracts

`examples/jira-xray.lms` is a service preset using the existing HTTP host. It adds
no Go host functions, schemas or external services. All seed values are synthetic.
It models the v1 Atlassian MCP gateway used by jiracli and selected Xray Cloud v2
operations, rather than replacing the separate Jira REST example.

## Start and reset

Build lmmock and install lmscript 0.2.454 or later. From the checkout, one command
starts the preset and prints its ephemeral loopback URL:

```sh
PATH="$PWD/build:$PATH" lmscript examples/jira-xray.lms node -e 'console.log(process.env.LMMOCK_URL);setInterval(()=>{},1000)'
```

The wrapper stops the listener when the child exits. For standalone serve and a
ready file, use the repository's trusted test fixture adapter:

```sh
node scripts/capture-fixture.mjs examples/jira-xray.lms build/jira-xray.lmcode
build/lmmock serve --lmcode build/jira-xray.lmcode --ready-file build/cloud-ready.json
```

Restart to reset all state, or POST `/__reset` using the synthetic MCP credential.
GET `/__state` exposes the current synthetic issues, pages, test steps and runs
with that same credential. Do not configure an upstream for this isolated preset.
State is per server, in memory, and lost on restart. Operations use existing
synchronized get/set primitives; multi-request read/modify/write sequences are
not transactions. Use sequential clients for deterministic write assertions.

## Authentication and protocol

| Endpoint | Request | Response |
| --- | --- | --- |
| `GET /.well-known/oauth-authorization-server` | None | Issuer, authorization/token/registration URLs, S256 and grant types |
| `POST /v1/register` | Client registration JSON | 201, client_id `fixture-client` |
| `GET /v1/authorize` | client_id, loopback redirect_uri, state, S256 code_challenge | 302 callback with one-use code and state |
| `POST /v1/token` | URL-encoded authorization_code + code_verifier, or refresh_token | access_token `fixture-token`, refresh_token `fixture-refresh`, token_type, expires_in |
| `POST /v1/mcp` | Bearer scheme with `fixture-token` | JSON-RPC 2.0; ID preserved |
| `POST /api/v2/authenticate` | JSON client_id `fixture-client`, client_secret `fixture-secret` | JSON string `fixture-xray-token` |
| `POST /api/v2/graphql` | Bearer scheme with `fixture-xray-token`; JSON query + variables | data or GraphQL errors envelope |

Credentials are deliberately low-entropy test values, not vendor credentials.
The OAuth stub checks PKCE/redirect binding and consumes each authorization code
once; it is not a production identity provider. Registration, consent, code
expiry and refresh rotation are simplified. Missing/wrong/expired credentials
return HTTP 401. A pre-seeded credential is sufficient; interactive login is not
required.

MCP `initialize` returns protocolVersion `2025-06-18`, tools capability,
serverInfo and a Mcp-Session-Id header. `notifications/initialized` returns empty
202; `tools/list` advertises the selected tool schemas; `tools/call` dispatches
`params.name` and `params.arguments`. Results use:

```json
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{\"key\":\"TEST-1\"}"}],"isError":false}}
```

Tool failures use isError=true and JSON text with error/message/statusCode, errorMessages and
errors. Unknown methods/tools use JSON-RPC -32601/-32602. The selected shape is
compatible with JSON responses. Config `mcpSse: true` streams each result as an
SSE message event and closes the response. JSON-RPC error responses are always
plain JSON, even in SSE mode. Long-lived SSE, session expiry, protocol
negotiation and arbitrary malformed input validation are not modeled.

## Jira tools

All issue tools accept cloudId (synthetic site `mock-site.example.test` or UUID
`00000000-0000-4000-8000-000000000001`). Cloud routing/permissions are not modeled.

| Tool | Arguments consumed | Behavior |
| --- | --- | --- |
| atlassianUserInfo | none | account_id/name/email profile shape |
| getAccessibleAtlassianResources | none | One synthetic site |
| lookupJiraAccountId | searchString | data.users.users/groups/statusCode envelope; search is simplified |
| getJiraIssue | issueIdOrKey, fields, expand | Key or numeric ID; fields projection, default all seeded fields; expand names/renderedFields subsets |
| searchJiraIssuesUsingJql | jql, maxResults, fields, nextPageToken or startAt | Reads current state; returns issues, nextPageToken/isLast and diagnostic total/startAt/maxResults |
| getTransitionsForJiraIssue | issueIdOrKey | Available transitions for current status |
| transitionJiraIssue | issueIdOrKey, transition.id, fields | To Do → 21 → In Progress → 31 → Done; invalid transitions fail |
| addCommentToJiraIssue | issueIdOrKey, commentBody | Appends a comment, visible on later get |
| createJiraIssue | projectKey, issueTypeName, summary, description, additional_fields | Creates the next TEST key; readable and searchable |
| editJiraIssue | issueIdOrKey, fields | Persists selected fields |

Write tools other than `createJiraIssue` return the full updated issue as a
diagnostic convenience; the vendor returns the created comment or an empty
result. Clients that only check `isError` and, for create, `key` are unaffected.

Seeds: TEST-1 and TEST-2 Tasks, TEST-3 Manual Test (issue ID 10003), TEST-4 Test
Execution (10004). The names expansion uses identity field labels; renderedFields is a canned synthetic
HTML description, not a Markdown renderer. Other expansion names are echoed only.
Missing issues return tool error statusCode 404. Empty required
create/comment values, unavailable transitions and unsupported JQL return 400.

JQL supports case-insensitive project/key/status equality, quoted `text ~`, AND,
and ORDER BY key ASC/DESC or updated ASC/DESC. Key ordering follows numeric seed
and insertion order; updated ordering is an insertion-order approximation (fixed
fixture timestamps). Keywords require single spaces around AND/ORDER BY;
operators tolerate surrounding spaces. Quoted values containing AND, embedded
quotes, OR, parentheses, functions and other operators are unsupported. They
must not be interpreted as a general JQL engine. Page size is 1–100; numeric
nextPageToken is a fixture cursor, not a vendor cursor. Newly created items join
searches. Extra total/startAt fields are diagnostic additions to the observed
nextPageToken/isLast response.

## Confluence reads

| Tool | Arguments | Response |
| --- | --- | --- |
| getConfluencePage | pageId, contentFormat markdown/html | id/title/body/version/spaceId/_links |
| searchConfluenceUsingCql | cql, limit | results with nested content/title/excerpt/url |
| getConfluenceSpaces | keys, limit | results of synthetic DEMO space (20000) |
| getPagesInConfluenceSpace | spaceId, limit, cursor | results and _links.next cursor |
| getConfluencePageDescendants | pageId | Demo home (20001) has child (20002) |
| getConfluencePageFooterComments | pageId | One synthetic comment on the home page |
| getConfluencePageInlineComments | pageId | Empty list |
| getConfluenceCommentChildren | commentId | Empty list |

CQL is a selected fixture selector for type=page/space and a simple trailing text
term, not a parser or general search engine. Page/space/children/comment list
responses preserve results/_links shapes; many vendor metadata fields are
omitted. Confluence write tools (createConfluencePage, updateConfluencePage,
createConfluenceFooterComment, createConfluenceInlineComment) exist in the client
but are outside this preset's read-tool subset.

## Xray operations

Requests use JSON `{query, variables}` with the client query templates:

| Field | Variables | Result/state |
| --- | --- | --- |
| getTest(issueId:$id) | id | issueId, testType{name,kind}, steps{id,action,data,result}; missing test=null |
| getTestExecution(issueId:$id).testRuns(limit:$limit,start:$start) | id, limit, start | issueId/testRuns{total,results}; two distinct seeded runs |
| addTestStep(issueId:$id,step:$s) | id, s{action,data,result} | Appends and returns the new step |
| updateTestRunStatus(id:$id,status:$status) | id, status | Boolean confirmation; later run status changes |
| updateTestRunComment(id:$id,comment:$comment) | id, comment | Boolean confirmation; comment persisted |
| addEvidenceToTestRun(id:$id,evidence:$ev) | id, ev[] | warnings array; synthetic evidence retained in memory |

Run IDs are visibly synthetic `000000000000000000000001` and
`000000000000000000000002`. Key→numeric ID resolution is performed through MCP
getJiraIssue, not a separate Xray operation. GraphQL dispatch recognizes these
selected field templates, not arbitrary aliases, fragments, multiple root
operations or selection projection. Diagnostic run objects include comment and
evidence even when the client's selection omits them; use `/__state` for fixture
inspection, not as a vendor endpoint. Unknown operations/invalid writes return
HTTP 200 `{data:null,errors:[{message:...}]}`. Evidence is not uploaded anywhere.

## Faults and tests

Authenticated requests accept X-Lmmock-Fault values 403, 429 or 500. The 429 reply
sets Retry-After: 1. Optional config `runPageSize` caps Xray pages to exercise client pagination.
Config `{"failures":2,"failureStatus":503,"delayMs":10}` fails
the first two requests per MCP/GraphQL path, then succeeds; delay is milliseconds.
Use the ordinary --config option in standalone mode. Reset clears counters too.
Faults occur before mutations, so denied requests do not change fixture state.
Non-JSON request bodies return HTTP 500 from the host, not a JSON-RPC parse error.

`lmscript test.lms` compiles this preset and runs HTTP-level Go contract tests
alongside the other fixtures. Tests require no jiracli installation, credentials
or live network. `go test ./internal/mock -run TestCloud -count=1` runs only these
contracts after `scripts/compile-examples.sh` has generated fixtures.

## Using jiracli

The inspected CLI supports JIRACLI_HOME and JIRA_CLOUDID, but has no CLI/env
base-URL override. The MCP/Xray clients expose constructor test options, while
CLI entry points use hard-coded endpoint constants. Until jiracli exposes
test-only URL overrides for the MCP, OAuth discovery and Xray endpoints, a
CLI-level test needs a throwaway source copy with only those endpoint constants
redirected to the loopback preset. A jiracli follow-up should add loopback-only
`JIRACLI_MCP_URL`, `JIRACLI_OAUTH_DISCOVERY_URL`, `XRAY_AUTH_URL` and
`XRAY_GRAPHQL_URL` overrides; it is outside this preset change.
Never patch an operator's normal checkout or route real credentials to this mock.

Use a private 0700 JIRACLI_HOME and 0600 credentials.json, version 1, with client
clientId `fixture-client`, token accessToken `fixture-token`, refreshToken
`fixture-refresh`, a future expiresAt, and site.host `mock-site.example.test`.
Set only synthetic Xray client values as listed above. Block non-loopback fetches
in the test copy. Draft → preview → submit can then exercise local-only writes;
keep that approval gate in the client. This mock itself does not implement a
preview or irreversible-action gate because all its writes are ephemeral.

Protocol sources: jiracli's MCP, OAuth, wiki, Xray and form modules.
Public references: [Atlassian MCP tools](https://developer.atlassian.com/cloud/rovo-mcp/guides/supported-tools/)
and [Xray GraphQL schema](https://xray.cloud.getxray.app/doc/graphql/).
Only synthetic structures are included here; no live captures are fixtures.
