# OAuth 2 authorization-code + PKCE mock

The fixture validates the registered redirect URI (including RFC 8252 ephemeral loopback
ports `http://127.0.0.1:<port>/callback`), client ID, required state and S256
challenge; authorization responds with a 302 to the registered callback and echoes state
(or redirects with `?error=...&state=...` per RFC 6749 §4.1.2.1).
`POST /oauth/token` accepts both POST body credentials and HTTP Basic authentication
(`Authorization: Basic`), consumes the authorization code once, and verifies the RFC 7636
`code_verifier` (43–128 characters in `[A-Za-z0-9-._~]`).
The same endpoint supports a synthetic `client_credentials` grant as the plain token
endpoint. All client secrets and access tokens are fake fixture strings.

The registered callback is a documentation-only client address; tests inspect the 302
Location and do not open a real browser or contact an identity provider.
