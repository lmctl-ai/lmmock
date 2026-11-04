# OAuth 2 authorization-code + PKCE mock

The fixture validates the registered redirect URI, client ID, required state and S256
challenge; authorization responds with a 302 to the registered callback and echoes state.
`POST /oauth/token` consumes the authorization code once and verifies the `code_verifier`.
The same endpoint supports a synthetic `client_credentials` grant as the plain token
endpoint. All client secrets and access tokens are fake fixture strings.

The registered callback is a documentation-only client address; tests inspect the 302
Location and do not open a real browser or contact an identity provider.
