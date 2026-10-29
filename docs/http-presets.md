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

## Twilio, SendGrid and Mailchimp (Wave 9)

These selected contracts were mined from n8n commit
`dd5f870ecc38344951fb3956f1a7ba7852612c70`, under `packages/nodes-base`:

| Preset | Source files inspected | Routes and limits |
| --- | --- | --- |
| twilio | `credentials/TwilioApi.credentials.ts`, `nodes/Twilio/GenericFunctions.ts`, `nodes/Twilio/Twilio.node.ts` | GET `/2010-04-01/Accounts/{sid}.json`; POST `Messages.json` and `Calls.json` under the account; GET each created resource by SID. Form encoding, Basic AccountSID:AuthToken. |
| sendgrid | `credentials/SendGridApi.credentials.ts`, `nodes/SendGrid/GenericFunctions.ts`, `nodes/SendGrid/SendGrid.node.ts`, `nodes/SendGrid/test/SendGrid.node.test.ts`, `nodes/SendGrid/__schema__/v1.0.0/list/create.json` | GET `/v3/scopes`; POST `/v3/mail/send` returns empty 202; GET/POST `/v3/marketing/lists`; GET a list by ID. Bearer auth, JSON bodies. |
| mailchimp | `credentials/MailchimpApi.credentials.ts`, `nodes/Mailchimp/GenericFunctions.ts`, `nodes/Mailchimp/Mailchimp.node.ts` | GET `/3.0/lists` (credential test); GET campaigns and a campaign by ID; POST campaign `actions/replicate` and `actions/send` (empty 204). n8n's `apikey` auth header, JSON bodies. |

Twilio defaults to `ACfixture:fixture-token`; override with config fields
`accountSid` and `authToken`. SendGrid defaults to `fixture-token`, Mailchimp to
`fixture-token-us1`; each accepts a `token` config override. All are dummy values.
These presets accept only credential config knobs. `X-Lmmock-Fault: 429` on an
authenticated request selects the deterministic rate-limit response.

Twilio SID values are synthetic, voice/media validation is minimal, and no
messages or calls are delivered. API-key SID authentication, Twilio events,
webhooks and status transitions are deferred. SendGrid uses a single-page list
snapshot with empty `_metadata` (no `next`), as required to terminate n8n's loop.
Created lists support read-back but do not join that snapshot. Mail accepts a
minimal required-field check; templates, contact search and real delivery are
not modeled.

Mailchimp uses n8n's actual `apikey` header and `/lists` credential test, rather
than Basic auth and `/ping`. Datacenter selection is replaced by loopback;
OAuth and Basic auth are outside this selected n8n contract. List snapshots
contain one item at offset zero and terminate with an empty page at positive
offset, matching n8n's offset loop. Count/filter behavior is not general.
`campaign-fixture` is the seed campaign; replicas support read-back and sending
updates their local status only. No actual campaign delivery or trigger flows.

Errors preserve vendor envelopes and statuses with simplified deterministic
messages, based on [Twilio responses](https://www.twilio.com/docs/usage/twilios-response),
[Twilio 20429](https://www.twilio.com/docs/api/errors/20429),
[SendGrid errors](https://www.twilio.com/docs/sendgrid/api-reference/how-to-use-the-sendgrid-v3-api/errors),
and [Mailchimp errors](https://mailchimp.com/developer/marketing/api-concepts/errors).
Suites cover missing/wrong auth, faults, missing resources and read-after-create.
They run both through the native checker and the real lmconn HTTP CLI; they do
not execute n8n or contact the live vendors.

## Bitbucket, CircleCI and PayPal (Wave 9)

These presets add selected contracts, not complete vendor coverage. Their native
and real-lmconn suites check happy paths, creation/read-back, missing resources,
invalid arguments, missing/wrong credentials and authenticated fault responses.
Credential overrides are the only configuration knobs. All defaults are dummy values.

| Preset | Routes | Configuration |
| --- | --- | --- |
| bitbucket | GET /2.0/user; GET /2.0/user/workspaces; GET /2.0/repositories/{workspace}; GET/POST /2.0/repositories/{workspace}/{repo}/hooks | username=fixture-user, appPassword=fixture-password; HTTP Basic |
| circleci | GET /api/v2/me; GET/POST /api/v2/project/{vcs}/{slug}/pipeline; GET same path/{number} | token=fixture-token; Circle-Token header |
| paypal | POST /v1/oauth2/token; GET /v1/identity/oauth2/userinfo; POST /v1/payments/payouts; GET same path/{id}; GET /v1/notifications/webhooks-event-types | clientId=fixture-client, secret=fixture-secret, token=fixture-access-token |

Bitbucket models n8n's legacy Basic app-password path. `/2.0/user` is the actual
node credential test. Workspace `fixture` and repository `demo` are fixed.
Repository pages contain two canned snapshots, with a loopback `next` URL on the
first page and no `next` on the last. Native adversarial checks follow the returned
URL and assert termination; suites check both pages. Hook creation stores the
latest hook for list/read-back; multiple hooks, deletion, real callback delivery,
workspace hooks and access-token authentication remain deferred. Hook IDs are
synthetic strings rather than vendor UUIDs. Error text is simplified within the
Bitbucket `type:error` / `error.message` envelope. X-Lmmock-Fault: 429 triggers
an authenticated rate-limit response with Retry-After: 1.

CircleCI uses the `/api/v2` base from n8n (not `/v2` alone). Its project is
`gh/fixture/demo`, with `fixture%2Fdemo` encoded as one path segment exactly as
CircleCi.node.ts constructs it. List pagination returns `next_page_token` and
terminates at null; suites reuse that token. Triggered pipelines are stored by
number for read-back but not added to list snapshots. There is no job execution,
workflow API, arbitrary filtering or permission model. Branch and tag together
are rejected; tags are accepted but tag-specific execution semantics are not
modeled. `/me` is an identity probe: this n8n credential class defines no test.
X-Lmmock-Fault: 429 returns a message envelope and Retry-After: 1.

PayPal requires Basic clientId:secret plus Content-Type application/x-www-form-urlencoded
and `grant_type=client_credentials` in the form body. JSON grants and other grants
are rejected. The issued token is captured and reused by both contract clients.
Protected routes reject tokens before issuance. Tokens are deterministic and do
not expire; scopes, refresh, multiple clients and real money movement are absent.
Payout batches preserve submitted headers/items and return terminal empty links;
there is no payout processing, item lifecycle or multi-page batch pagination.
The userinfo route is a synthetic identity probe, not proof that a production
client-credentials token has user-consent scopes; n8n itself does not call it.
The credential class defines no test; the token exchange is its actual auth path.
X-Lmmock-Fault: 500 returns an authenticated INTERNAL_SERVER_ERROR envelope.
Webhook event enumeration is canned; subscription and signature verification
remain deferred. No external URL is fetched by any of these scripts.

Source basis: local n8n commit `dd5f870ecc38344951fb3956f1a7ba7852612c70`, paths
under packages/nodes-base:

- credentials/BitbucketApi.credentials.ts; nodes/Bitbucket/GenericFunctions.ts,
  BitbucketTrigger.node.ts and test/GenericFunctions.test.ts.
- credentials/CircleCiApi.credentials.ts; nodes/CircleCi/GenericFunctions.ts
  and CircleCi.node.ts.
- credentials/PayPalApi.credentials.ts; nodes/PayPal/GenericFunctions.ts,
  PayPal.node.ts and PayPalTrigger.node.ts.

Error-envelope and endpoint references supplement the n8n request sources:
[Bitbucket REST errors](https://developer.atlassian.com/cloud/bitbucket/rest/intro/),
[CircleCI v2](https://circleci.com/docs/api/v2/),
[PayPal authentication](https://developer.paypal.com/api/rest/authentication),
[PayPal payouts](https://developer.paypal.com/api/payments.payouts-batch/v1).

Bitbucket pagination needs the Host-header accessor correction shipped with these
fixtures. Older hosts returned an empty Host, producing an invalid absolute next
URL. Use a host built from this change or a later release for pagination fidelity.

## Dropbox, Box and Cloudflare subsets

| Script | Routes | Fixture limits |
| --- | --- | --- |
| dropbox.lms | POST users/get_current_account, files/list_folder, files/list_folder/continue, files/create_folder_v2, files/upload, files/download (all under /2) | Two fixed list pages; upload/download stores text by path; no binary fidelity, metadata completeness, copy/move/delete or sharing |
| box.lms | GET /2.0/users/me, GET /2.0/search, POST /2.0/folders, GET /2.0/folders/{id}, GET /2.0/files/{id} | Search is one fixed folder followed by an empty page for n8n's limit=100/offset=100 loop; filters and arbitrary page sizes are not modeled; no uploads or BoxTrigger webhooks |
| cloudflare.lms | GET /client/v4/user/tokens/verify, GET /client/v4/zones; list/upload/read/delete under /client/v4/zones/{zone}/origin_tls_client_auth | Two fixed certificate pages; uploaded certificates support read/delete; no PEM validation, real private-key handling, zone permissions or DNS operations |

These three presets accept only a `token` configuration override, defaulting to
`fixture-token`. Use `X-Lmmock-Fault: 500` for their deterministic fault response.
Dropbox and Box reject missing/wrong credentials with 401; Cloudflare uses the
403 authentication envelope with code 10000. Cloudflare's token-verification
endpoint-specific invalid-token codes are not modeled. Its other numeric error
codes and all error messages are simplified fixture values. Dropbox missing
files/cursors use endpoint-specific 409 envelopes; malformed required arguments
use 400. Box uses the `type/status/code/message/request_id` error envelope.
Cloudflare never returns or stores the supplied private key.

Dropbox's identity endpoint accepts an omitted or literal null body. Upload and
download support both the `Dropbox-API-Arg` header and the `arg` query parameter
used by this n8n snapshot. The API and content hosts share the local mock origin.
Box's OAuth credential has no explicit credential-test request in this snapshot;
`users/me` is the identity probe. Cloudflare's node operates on origin-pull
certificates, not DNS. List results are fixed snapshots, not views of writes.
Malformed JSON parsing, full response metadata, OAuth issuance and real vendor
permission models remain outside these subsets.

Source inspection: n8n commit `dd5f870ecc38344951fb3956f1a7ba7852612c70`, under
`packages/nodes-base/`:

- `credentials/DropboxApi.credentials.ts`, `DropboxOAuth2Api.credentials.ts`,
  `nodes/Dropbox/GenericFunctions.ts`, `Dropbox.node.ts`.
- `credentials/BoxOAuth2Api.credentials.ts`, `nodes/Box/GenericFunctions.ts`,
  `Box.node.ts`.
- `credentials/CloudflareApi.credentials.ts`, `nodes/Cloudflare/GenericFunctions.ts`,
  `Cloudflare.node.ts`, `ZoneCertificateDescription.ts`.

Error references: [Dropbox error guide](https://docs.dropboxapi.com/dropbox-api/docs/error-handling),
[Box common errors](https://developer.box.com/guides/api-calls/permissions-and-errors/common-errors),
[Cloudflare authentication errors](https://developers.cloudflare.com/terraform/troubleshooting/authentication-error-dns-records/).
Suites exercise the native mock checker and lmconn HTTP CLI, not n8n itself or
live vendor accounts.
