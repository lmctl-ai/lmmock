# Standard `lmcode_std_v3` REST & CLI Interfaces in `lmmock`

`lmmock` has been upgraded to `github.com/lmctl-ai/lmcode-go` (`2027887`, 226 standard signatures) and supports both its host-native `Handler(Exchange) -> void` interface and all 10 standard `lmcode_std_v3` REST and CLI interfaces:

- **HTTP Method Interfaces**: `RestGet`, `RestPost`, `RestPut`, `RestDelete`, `RestPatch`, `RestHead`, `RestOptions` (all `(RestExchange) -> void`)
- **Generic Route Interface**: `RestHandler(RestExchange) -> void`
- **CLI Interfaces**: `CliCommand(RestExchange) -> void` and `CliOption(RestExchange) -> void`
- **Top-Level Configuration & Dispatch**:
  - `rest_config(key, value)` sets shared state (`x.get(key)`) and configures `"prefix"` / `"base_path"` for route registration.
  - `rest_route(method, path, annotation)` registers routes alongside `route(method, path, annotation)`.
  - `rest_cli(args)` dispatches CLI arguments against `CliOption`, `CliCommand`, and registered HTTP routes.
  - `:param`, `:param...`, and `*param` path segments are automatically normalized to Go `net/http.ServeMux` `{param}` and `{param...}` patterns.
  - `lmmock --lmcode FILE [CLI args...]` executes stateless CLI commands directly, and `lmmock --lmcode-signatures` outputs the full host + `lmcode_std_v3` signature list.

## Verification

- `internal/mock/mock_test.go` (`TestStandardRestAndCliInterfaces`): compiles and verifies `CliOption` (`@"--env"`), `RestGet` (`@"/users/:id"`), `RestPost` (`@"/users/:id"`), `CliCommand` (`@ping`), and `rest_config("prefix", "/api")` across both `e.CLI(...)` and live HTTP (`e.ServeHTTP`).
