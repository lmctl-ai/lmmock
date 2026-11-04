# Loopback authorization mocks

These examples are synthetic loopback identity providers for lmauth and client
compatibility tests. They do not contact GitHub or npmjs and contain no real
application secrets or access tokens. Build the handlers through the normal example
compiler and run them with the lmmock binary:

```sh
bash scripts/compile-examples.sh
node tests/auth-mocks/run.mjs build/lmmock
```

The release test workflow builds lmmock first and runs the same test runner on its
platform matrix. The tests assert complete request/response behavior, not just that a
port opens. They also compile three deliberately broken handler copies and require the
protocol assertions to catch each mutation.

## GitHub device authorization

`examples/auth/github-device-flow/server.lms` mocks `POST /login/device/code`,
`POST /login/oauth/access_token`, `GET /user`, and `GET /repos/{owner}/{repo}`.
Device-code responses are JSON when `Accept` requests JSON and form-encoded otherwise.
The poll endpoint exercises `authorization_pending`, `slow_down` with a five-second
interval increase, `expired_token`, `access_denied`, and the approved synthetic Bearer
token. The REST routes return `X-OAuth-Scopes` and `X-Accepted-OAuth-Scopes` for classic/OAuth
tokens, omit `X-OAuth-Scopes` for fine-grained PATs (`fineGrainedToken` or `"tokenKind": "fine-grained"`),
emit `X-Accepted-GitHub-Permissions` and `github-authentication-token-expiration` (`YYYY-MM-DD HH:MM:SS UTC`),
and serve both private (`fixture-org/fixture-repo`) and public (`lmctl-ai/lmpdf`, `fixture-org/public-repo`)
repository responses with a `permissions` object.

The `GET /login/device` page and `POST /__mock/approve` control represent the user's
approval action. `POST /__mock/clock` is enabled only in the fixture config so interval
and expiry cases can be advanced deterministically without sleeps. These control routes
are lmmock test helpers, not GitHub API endpoints. The documented flow and response
fields follow GitHub's [OAuth device flow](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app).

`GET /user` and `GET /repos/{owner}/{repo}` shapes follow the public
[authenticated-user](https://docs.github.com/en/rest/users/users) and
[repository](https://docs.github.com/en/rest/repos/repos) REST APIs. GitHub documents
OAuth scope headers in [Scopes for OAuth apps](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/scopes-for-oauth-apps),
the fine-grained permission hint in its
[permissions reference](https://docs.github.com/en/rest/authentication/permissions-required-for-fine-grained-personal-access-tokens),
and `GitHub-Authentication-Token-Expiration` in its
[token-expiration announcement](https://github.blog/changelog/2021-07-26-expiration-options-for-personal-access-tokens/).

## OAuth 2 authorization code + PKCE

`examples/auth/oauth2-pkce/server.lms` registers one synthetic client and allow-lists both
`http://client.example.test/callback` and RFC 8252 ephemeral loopback callbacks
(`http://127.0.0.1:<port>/callback`). `/authorize` requires a state value and an S256
challenge, returns a 302 redirect with `?error=...&state=...` when parameters are invalid
after `client_id`/`redirect_uri` validation (RFC 6749 §4.1.2.1), and issues a one-time
code on success. The token endpoint supports both POST body credentials and HTTP Basic
authentication (`Authorization: Basic`), checks client ID, callback URI, code expiry,
one-time use, RFC 7636 `code_verifier` grammar (43–128 characters in `[A-Za-z0-9-._~]`),
and `BASE64URL(SHA256(code_verifier))`. It also supports a synthetic `client_credentials`
grant. The userinfo route accepts only tokens minted by this server.

## npm registry web login

`examples/auth/npm-web-login/server.lms` follows the npm registry web-login shape:
`POST /-/v1/login` returns absolute `loginUrl` and `doneUrl`; GET on the login URL
serves a mock approval form; GET on `doneUrl` returns `202` with `Retry-After` while
pending and `200 {"token":...}` once the test approval control is used. A completed
session cannot be replayed. Authenticated `GET /-/whoami`, `GET /-/npm/v1/user` and
`GET /-/npm/v1/tokens` return synthetic identity, profile and token-list values (both after
web login and directly with the configured fixture token).

These documented routes and payloads follow npm's
[registry authentication API](https://github.com/npm/registry/blob/main/docs/user/authentication.md)
and [profile API](https://github.com/npm/registry/blob/main/docs/user/profile.md). The
client behavior is confirmed from the official
[`npm-profile` source](https://github.com/npm/npm-profile/blob/main/lib/index.js), which
POSTs the login request, opens `loginUrl`, polls `doneUrl`, accepts `202` with
`Retry-After`, and requires `200` to contain a token. Token and profile introspection
shapes come from the registry documentation. npm CLI's
[`auth.js`](https://github.com/npm/cli/blob/latest/lib/utils/auth.js) delegates its
`--auth-type=web` path to that client library.

## Client captures

`node tests/auth-mocks/capture.mjs build/lmmock /path/to/lminspect` records local curl
exchanges through lminspect into `captures/auth/`. These fixture captures contain only
synthetic request bodies and no real provider traffic; `tests/auth-mocks/captures.test.mjs`
checks the route shapes and redacted authorization headers in CI. The GitHub CLI's OAuth
flow is not assumed to support arbitrary HTTP `GH_HOST` overrides; the documented
endpoints are exercised using curl and the native mock client instead. npm CLI supports
a custom `--registry`; endpoint behavior is exercised against the same loopback server.
lminspect sanitizes recorded headers and bodies before they are committed.
