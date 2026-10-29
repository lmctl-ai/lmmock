# n8n connector auth shapes (2026-10-29)

What mining `packages/nodes-base` for the Slack, Discord, Telegram, Jira and
Stripe nodes taught us, written down because it generalises to the next fifty
connectors.

## Where n8n keeps the truth

- `credentials/<Service>Api.credentials.ts` carries the `authenticate` block
  (header name and template, or `auth.username/password` for Basic) and a
  `test.request` (`baseURL` + `url`): that request is the credential test the
  UI runs, so a fixture must serve it first (`/api/users.profile.get`,
  `/users/@me/guilds`, `/getMe`, `/rest/api/2/myself`, `/v1/charges`).
- `nodes/<Service>/**/GenericFunctions.ts` (or `transport/*.ts`, `helpers.ts`)
  builds the URL and chooses JSON vs form bodies; the node file lists the
  endpoints by resource/operation.

## Common auth shapes

| Shape | Services here | Fixture technique |
|---|---|---|
| Bearer token header | Slack (`xoxb-`), Stripe (`sk_`), most SaaS | `secure_equal(x.header("Authorization"), "Bearer " + want)` |
| Vendor-prefixed header | Discord `Authorization: Bot <token>` | same, with the vendor prefix; a Bearer form must be rejected |
| HTTP Basic | Jira Cloud `email:apiToken` | `basic_id` / `basic_secret` helpers, both compared with `secure_equal` |
| Secret in the URL path | Telegram `/bot<TOKEN>/<method>`, Discord webhooks `/webhooks/{id}/{token}` | route wildcards; Go's ServeMux needs a whole segment per wildcard, so split `bot<TOKEN>` inside the handler |

The later presets also cover a custom Private-Token header (GitLab) and
key + token query parameters (Trello). Notion requires a Notion-Version
header in addition to Bearer auth. See the [Handler and expansion guide](adding-n8n-http-services.md)
for the complete implementation and validation workflow.

## Error shapes are part of the contract

Clients branch on them, so a fixture must reproduce the vendor's shape, not a
generic one: Slack reports auth and argument errors as HTTP 200
`{"ok":false,"error":"..."}` and only rate limits as 429; Telegram wraps
everything in `{"ok":false,"error_code":N,"description":"..."}` with
`parameters.retry_after` on 429; Discord uses `{"message","code"}` with numeric
codes (10003 unknown channel, 50006 empty message, 50027 bad webhook token);
Jira uses `{"errorMessages":[],"errors":{}}`; Stripe uses
`{"error":{"type","message","code"?,"param"?}}` and never echoes the key.

## Body encodings

Stripe is form-encoded end to end (`form: body` in n8n); Slack and Telegram
accept JSON or form; Discord and Jira are JSON. A fixture reads both:
`str_starts_with(str_trim(body), "{")` picks `json_parse`, otherwise
`form_get`, falling back to the query string.

## Fault routes

One deterministic trigger per service (a magic channel, chat id, issue key or
amount) beats a config mode for retry tests: the suite can mix healthy and
failing calls in one run. 429 responses carry `Retry-After` because the real
services do and clients honour it.
