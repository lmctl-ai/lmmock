# npm registry web-login mock

Synthetic npm registry web login: `POST /-/v1/login` returns absolute `loginUrl` and
`doneUrl`; the mock approval form flips the pending session; `GET doneUrl` returns 202
with `Retry-After` while pending and 200 with a fixture token after approval. Authenticated
`GET /-/whoami`, `GET /-/npm/v1/user` and `GET /-/npm/v1/tokens` return documented registry
identity, profile and token shapes (including `name`, `readonly`, `automation` and
`cidr_whitelist`). The mock token is a fixture string, never an npm credential.

Route shapes follow npm's [registry authentication API](https://github.com/npm/registry/blob/main/docs/user/authentication.md)
and [profile API](https://github.com/npm/registry/blob/main/docs/user/profile.md), and the
current `npm-profile` client implementation used by the npm CLI. Control routes are
test-only; no npmjs requests are made.
