# Mock OAuth device-code login for the OpenAI Codex CLI

`--builtin oauth-codex-device` (source: examples/oauth-codex-device.lms) mimics the
auth service that `codex login --device-auth` talks to, so CI can test a real Codex
login with no browser, human, network, Node or compiler. Protocol verified against
codex-rs at tag rust-v0.166.0 (`login/src/device_code_auth.rs`, `server.rs`,
`token_data.rs`, `auth/manager.rs`) and against the real 0.166.0 binary. All secrets
are synthetic and are never echoed in error bodies.

~~~sh
lmmock serve --builtin oauth-codex-device --listen 127.0.0.1:18090 --audit-log audit.jsonl
lmmock exec  --builtin oauth-codex-device --config cfg.json -- YOUR_TEST
~~~

## Endpoints

| Route | Behavior |
| --- | --- |
| POST /api/accounts/deviceauth/usercode | JSON `{client_id}` -> 200 `{device_auth_id, user_code, interval}`. `interval` is a JSON **string**: codex 0.166 rejects a number. 404 if `disabled` (codex: "device code login is not enabled") |
| POST /api/accounts/deviceauth/token | JSON `{device_auth_id, user_code}` -> 403 (pending, `pendingStatus`) until approved, then 200 `{authorization_code, code_challenge, code_verifier}` with `code_challenge = base64url(SHA256(code_verifier))`. Unknown id / wrong user_code -> 400 |
| POST /oauth/token | form or JSON, by `grant_type` (below) |
| POST /oauth/revoke | JSON `{token,...}`; revokes a refresh token (sent by `codex logout`) |
| GET /codex/device | placeholder verification page |
| POST /__oauth/approve | JSON `{user_code}`: approve a pending login (`autoApprove:false`) |
| GET /__oauth | assertions: `{"counts":{usercode,poll,code_exchange,api_key_exchange,refresh,revoke,approve},"events":[{step,status,detail}]}` in order |

`/oauth/token` grants:

* `authorization_code`: code is single use; `client_id` must match, `redirect_uri` must end in
  `/deviceauth/callback`, and `SHA256(code_verifier)` must equal the issued challenge, else
  400 `invalid_grant` (the code is consumed). Returns `{id_token, access_token, refresh_token, token_type, expires_in}`.
  `id_token` and `access_token` are unsigned-style JWTs (fake signature) whose claims satisfy codex's
  `parse_chatgpt_jwt_claims`: `email`, `exp`, and `https://api.openai.com/auth` with
  `chatgpt_plan_type`, `chatgpt_user_id`/`user_id`, `chatgpt_account_id`.
* `urn:ietf:params:oauth:grant-type:token-exchange` (`requested_token=openai-api-key`, `subject_token`
  = an id_token this server issued, type `...:id_token`): 200 `{access_token:"sk-mock-..."}`, else 400.
  (Codex 0.166 does not call this during device login, only the browser flow does.)
* `refresh_token` (codex sends JSON; form also accepted): rotates the refresh token (unless
  `rotateRefreshTokens:false`). A replayed rotated token -> 401 `refresh_token_reused`, a revoked one
  -> 401 `refresh_token_invalidated`, unknown -> 401 `invalid_grant`. Codex treats 401 as permanent.

## Config (--config JSON, all optional)

| Key | Default | Meaning |
| --- | --- | --- |
| `mode` | `approve` | `pending-forever` (always 403), `denied` (400 `access_denied`, codex fails at once), `expired` (410 `expired_token`) |
| `autoApprove` | true | false: stay pending until `POST /__oauth/approve` |
| `pollsBeforeApproval` | 0 | auto-approve on poll N+1 |
| `pendingStatus` | 403 | 403 or 404, both mean "pending" to codex |
| `interval` / `intervalAsNumber` | 1 / false | polling seconds; number form is for negative tests |
| `userCode` | `MOCK-0001` | |
| `clientId` | any | when set, other client ids get 400/401 |
| `disabled` | false | usercode returns 404 |
| `email`, `planType`, `userId`, `accountId` | mock-user@example.test, plus, user-mock-0001, acct-mock-0001 | id_token claims |
| `expiresIn` | 3600 | token lifetime seconds; negative makes codex refresh on next use |
| `rotateRefreshTokens` | true | |
| `failApiKeyExchange` | false | token-exchange returns 400 (codex tolerates it) |
| `corruptVerifier` | false | poll hands out a verifier that fails PKCE, so codex's exchange gets 400 |

## Running the real Codex binary

Codex only talks to `https://auth.openai.com`, so route that host to the mock through an
HTTPS-intercepting proxy with a throwaway CA ([lminspect](https://github.com/lmctl-ai/lminspect)):

~~~sh
scripts/codex-login-e2e.sh /path/to/codex /path/to/lminspect [/path/to/lmmock]
~~~

The script runs `lminspect certgen`, then for each scenario starts the mock and
`lminspect run --ssl-bump --route auth.openai.com=http://127.0.0.1:PORT`, and runs
`codex login --device-auth` with an isolated `HOME`/`CODEX_HOME`, `HTTPS_PROXY`, `SSL_CERT_FILE`
and `CODEX_CA_CERTIFICATE` pointing at the CA. It asserts: approved login after pending polls
(`codex login status` reports logged in, `auth.json` holds id/access/refresh tokens, PKCE exchange seen),
refresh of an expired access token via `codex exec`, logout, bad PKCE verifier, `denied`,
`pending-forever` and `disabled`. Linux and macOS; needs curl and bash. `codex` prints a harmless warning
if `HOME` is under /tmp.

Offline Go tests: `go test ./cmd/lmmock -run OAuthCodex`.
