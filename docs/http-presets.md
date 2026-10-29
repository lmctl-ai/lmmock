# Service HTTP presets

Each .lms file specializes the generic HTTP host with annotated functions that
implement Handler. The Go host does not contain service-specific dispatch code.
Edit the handlers to change the fixture behavior.

| Script | Authentication | Implemented subset |
| --- | --- | --- |
| notion.lms | Bearer token and Notion-Version header | users/me, paged search, create/read pages |
| airtable.lms | Bearer token | meta/whoami, paged records, create/read a single record in appFixture/tblFixture |
| hubspot.lms | Bearer token | account-info/v3/details, paged CRM v3 contact search, create/read contacts |
| gitlab.lms | Private-Token header | personal_access_tokens/self, paged project 1 issues, create/read issues |
| asana.lms | Bearer token | users/me, paged tasks with next_page, create/read tasks |
| trello.lms | key and token query parameters | members/me, tokens/{token}/member, board-fixture cards, create/read cards, before pagination termination |

Default token: fixture-token. Trello also requires key=fixture-key.
Override these with --config pointing to {"token":"another-test-token","key":"another-test-key"}.
All credentials in the examples are synthetic.

Use X-Lmmock-Fault: 429 or X-Lmmock-Fault: 500 on an authenticated request to
produce a deterministic failure. The 429 response includes Retry-After: 1.
Missing/wrong credentials return 401. Unknown item IDs return 404. Missing
required create fields return 400. Notion also rejects a missing version header.

Creation stores the returned item under its generated ID. Reading that ID returns
the stored item. List/search pages are fixed snapshots; they do not include newly
created items. These fixtures test selected contracts, not full vendor APIs.
Other limitations include no update/delete, no general filtering, no vendor
permission model, no OAuth issuance inside these service scripts, and simplified
error messages. Airtable batch writes and Notion v3 data sources are not included.
Use oauth.lms to test the shared authorization flow.

## Run a suite

Install the compiler and run from the repository root:

    npm i -g @lmctl-ai/lmctl
    lmmock exec --script examples/notion.lms -- lmmock check --suite examples/notion-client.json

The same command works with any service name from the table. lmmock opens an
ephemeral loopback port, exports LMMOCK_URL to the client, and shuts down when the
client exits. The normal lmscript test.lms run compiles and checks every preset.
lmconn also vendors the scripts and suites and runs them through its actual HTTP
CLI on Linux, macOS and Windows.

## n8n source basis

The credential and request shapes were inspected in the local n8n checkout under
packages/nodes-base at commit 9f84b4b9f7b2744bb8df728c0caa6ed16a77fd2b:

- credentials/NotionApi.credentials.ts and nodes/Notion/v2/NotionV2.node.ts
- credentials/AirtableTokenApi.credentials.ts and nodes/Airtable/v2/transport/index.ts,
  actions/record/create.operation.ts
- credentials/HubspotAppToken.credentials.ts and nodes/Hubspot/__test__/Hubspot.node.test.ts
- credentials/GitlabApi.credentials.ts and nodes/Gitlab/GenericFunctions.ts
- credentials/AsanaApi.credentials.ts and nodes/Asana/GenericFunctions.ts
- credentials/TrelloApi.credentials.ts and nodes/Trello/GenericFunctions.ts

The CI suites use lmconn and the native lmmock client. They do not execute n8n
itself or call live vendor APIs.

## PagerDuty, Datadog and Linear selected contracts

| Script | Authentication / config | Routes or GraphQL operations |
| --- | --- | --- |
| pagerduty.lms | `Authorization: Token token=fixture-token`; override `token` | GET `/users/me`, GET `/services`, GET/POST `/incidents`, GET `/incidents/{id}` |
| datadog.lms | `DD-API-KEY: fixture-key`, `DD-APPLICATION-KEY: fixture-app-key`; override `apiKey` / `appKey` | GET `/api/v1/validate`, GET/POST `/api/v1/monitor`, GET `/api/v1/monitor/{id}` |
| linear.lms | raw `Authorization: fixture-key` or OAuth `Bearer fixture-token`; override `apiKey` / `token` | POST `/graphql`: issues, teams, issueCreate, issue |

All three support authenticated `X-Lmmock-Fault: 429` and `500`. Datadog's
validate endpoint needs only the API key; monitor routes need both keys and
return 403 `errors` arrays on auth failure. PagerDuty returns 401 with error code
2006. Linear returns GraphQL `errors` arrays (including HTTP 200 for a missing
issue); OAuth and API-key credentials are separate. Credential overrides are
the only config knobs. Every suite exercises missing and wrong credentials.

PagerDuty creates require a From header, incident title/type and service reference.
The incident snapshot has 100 records at offset 0 and one at offset 100, matching
n8n's fixed limit=100 pagination; other page sizes/filters are not implemented.
Datadog monitor pages are fixed at one item per page (0, 1, then empty), exercised
with page_size=1. Linear issues return one item plus pageInfo and an endCursor;
passing that cursor terminates the next page. Created items are retrievable by
captured ID but are not merged into snapshots. Datadog monitor queries are stored,
not evaluated. Linear matches selected query strings and variables; it is not a
GraphQL parser and does not implement aliases, fragments, selection projection,
inline mutation arguments, or arbitrary whitespace variations. Suites use the
actual n8n query templates. No OAuth issuance, webhooks, update/delete or complete
vendor permission model is included.

Source inspection: n8n commit `dd5f870ecc38344951fb3956f1a7ba7852612c70`,
under `packages/nodes-base/`:

- PagerDuty: `credentials/PagerDutyApi.credentials.ts`,
  `nodes/PagerDuty/GenericFunctions.ts`, `nodes/PagerDuty/PagerDuty.node.ts`.
  This credential has no test method; `/users/me` is the chosen identity check.
- Datadog: `credentials/DatadogApi.credentials.ts`. This checkout has no Datadog
  node: it exposes a generic HTTP-request credential. `/api/v1/validate` is its
  credential test; the monitor subset is an additional HTTP-request contract,
  not a claim that a dedicated n8n node exercises those routes.
- Linear: `credentials/LinearApi.credentials.ts`, `LinearOAuth2Api.credentials.ts`,
  `nodes/Linear/GenericFunctions.ts`, `Queries.ts`, and
  `test/GenericFunctions.test.ts`. The credential test uses getIssues(first:1),
  not viewer. API keys are sent without a Bearer prefix.

Supplementary vendor references: [PagerDuty errors](https://docs.pagerduty.com/developer/errors),
[Datadog monitors](https://docs.datadoghq.com/api/latest/monitors/),
[Datadog authentication](https://docs.datadoghq.com/api/latest/authentication/),
and [Linear rate limiting](https://linear.app/developers/rate-limiting).
Error envelopes and statuses follow these contracts; messages are simplified.
