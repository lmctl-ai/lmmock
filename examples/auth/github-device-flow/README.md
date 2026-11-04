# GitHub device-flow mock

Synthetic GitHub OAuth device authorization and REST inspection. The flow uses a mock
approval page (`GET /login/device`) and a test/operator control (`POST /__mock/approve`);
no GitHub account, app secret or real token is involved. The clock control is enabled by
the example config so tests can prove interval and expiry behavior without sleeping.

Routes:

- `POST /login/device/code`: form or JSON request; JSON response when `Accept` includes
  `application/json`, otherwise URL-encoded fields.
- `POST /login/oauth/access_token`: `authorization_pending`, `slow_down` (+ five seconds),
  `expired_token`, `access_denied`, or a synthetic Bearer token.
- `GET /user` and `GET /repos/{owner}/{repo}`: fake identity, OAuth scope/expiration
  headers, accepted permissions and the repository `permissions` object.
- `POST /__mock/clock`: test-only `{"advance_seconds":N}`.

Wire shapes follow GitHub's public [OAuth device flow](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app)
and [REST user/repository APIs](https://docs.github.com/en/rest/users/users). The control
routes and fixture identities are synthetic lmmock behavior.
