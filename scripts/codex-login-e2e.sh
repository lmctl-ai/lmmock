#!/usr/bin/env bash
# Real-client acceptance: `codex login --device-auth` against --builtin oauth-codex-device.
#   scripts/codex-login-e2e.sh CODEX_BIN LMINSPECT_BIN [LMMOCK_BIN]
# lminspect (github.com/lmctl-ai/lminspect) is an HTTPS-intercepting proxy: it terminates TLS
# for auth.openai.com with a throwaway CA (made by `lminspect certgen`) and forwards to the mock.
# No Node, compiler or network access is needed. Linux and macOS.
set -uo pipefail
CODEX=$(cd "$(dirname "$1")" && pwd)/$(basename "$1"); INSPECT=$(cd "$(dirname "$2")" && pwd)/$(basename "$2")
ROOT=$(cd "$(dirname "$0")/.." && pwd)
LMMOCK=${3:-$ROOT/build/lmmock}
[ -x "$LMMOCK" ] || { LMMOCK=$(mktemp -d)/lmmock; (cd "$ROOT" && go build -o "$LMMOCK" ./cmd/lmmock) || exit 2; }
WORK=$(mktemp -d); PIDS=(); FAILED=0
cleanup() { for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null; done; wait 2>/dev/null; }
trap cleanup EXIT
free_port() { python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])' 2>/dev/null || echo $((20000 + RANDOM % 20000)); }
pass() { echo "PASS $1"; }
fail() { echo "FAIL $1"; FAILED=1; }
check() { local name=$1; shift; if "$@" >/dev/null 2>&1; then pass "$name"; else fail "$name"; fi; }

"$INSPECT" certgen -cert "$WORK/ca.crt" -key "$WORK/ca.key" >/dev/null || exit 2

# start_stack NAME CONFIG_JSON -> sets MOCK_URL, PROXY, HOME_DIR
start_stack() {
  local name=$1 config=$2 mp ip
  mp=$(free_port); ip=$(free_port)
  HOME_DIR="$WORK/$name/home"; mkdir -p "$HOME_DIR/.codex"
  printf '%s' "$config" >"$WORK/$name/cfg.json"
  "$LMMOCK" serve --builtin oauth-codex-device --config "$WORK/$name/cfg.json" --listen "127.0.0.1:$mp" \
    --audit-log "$WORK/$name/audit.jsonl" >"$WORK/$name/mock.log" 2>&1 & PIDS+=($!)
  "$INSPECT" run --listen "127.0.0.1:$ip" --ssl-bump --ca-cert "$WORK/ca.crt" --ca-key "$WORK/ca.key" \
    --route "auth.openai.com=http://127.0.0.1:$mp" --record "$WORK/$name/flows.jsonl" >"$WORK/$name/inspect.log" 2>&1 & PIDS+=($!)
  MOCK_URL=http://127.0.0.1:$mp; PROXY=http://127.0.0.1:$ip
  for _ in $(seq 50); do curl -fs "$MOCK_URL/__oauth" >/dev/null 2>&1 && grep -q listening "$WORK/$name/inspect.log" && break; sleep 0.2; done
}
codex() { # codex ARGS... (isolated HOME, all TLS via the proxy and the throwaway CA)
  HOME="$HOME_DIR" CODEX_HOME="$HOME_DIR/.codex" HTTPS_PROXY="$PROXY" https_proxy="$PROXY" NO_PROXY= no_proxy= \
    SSL_CERT_FILE="$WORK/ca.crt" CODEX_CA_CERTIFICATE="$WORK/ca.crt" "$CODEX" "$@"
}
run_limited() { # run_limited SECONDS CMD...: run CMD, kill it after SECONDS
  local secs=$1 pid; shift
  "$@" & pid=$!
  for _ in $(seq $((secs * 10))); do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
  kill "$pid" 2>/dev/null; wait "$pid" 2>/dev/null
}
login() { # login SECONDS: run the device login, stop it after SECONDS; output in $OUT, exit status in $RC
  local log="$WORK/login.out" pid
  codex login --device-auth >"$log" 2>&1 </dev/null & pid=$!
  for _ in $(seq $(($1 * 10))); do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
  if kill -0 "$pid" 2>/dev/null; then kill "$pid" 2>/dev/null; wait "$pid" 2>/dev/null; RC=124; else wait "$pid"; RC=$?; fi
  OUT=$(tr -d '\0' <"$log" | sed 's/\x1b\[[0-9;]*m//g')
}
state() { curl -fs "$MOCK_URL/__oauth"; }
stop_stack() { for p in "${PIDS[@]:-}"; do kill "$p" 2>/dev/null; done; PIDS=(); sleep 0.3; }

AUTH_APP=app_EMoamEEZ73f0CkXaXp7hrann
out_has() { printf '%s' "$OUT" | grep -q -- "$1"; }
out_lacks() { ! out_has "$1"; }
state_has() { local o; o=$(state); printf '%s' "$o" | grep -Eq -- "$1"; }
status_has() { local o; o=$(codex login status 2>&1); printf '%s' "$o" | grep -q -- "$1"; }
file_has() { grep -q -- "$2" "$1"; }
refresh_status() { curl -s -o /dev/null -w '%{http_code}' -X POST "$MOCK_URL/oauth/token" -H 'content-type: application/json' \
  -d "{\"client_id\":\"$AUTH_APP\",\"grant_type\":\"refresh_token\",\"refresh_token\":\"$1\"}"; }
is() { [ "$1" = "$2" ]; }

echo "== happy path: approved after 2 pending polls, PKCE verified, refresh round trip"
start_stack happy '{"pollsBeforeApproval":2,"interval":1}'
login 60
check "login exits 0" is "$RC" 0
check "login status reports logged in" status_has 'Logged in'
AUTH="$HOME_DIR/.codex/auth.json"
check "auth.json has id_token" file_has "$AUTH" '"id_token"'
check "auth.json has access_token" file_has "$AUTH" '"access_token"'
check "auth.json has refresh_token" file_has "$AUTH" '"refresh_token"'
check "mock saw usercode, 3 polls, one PKCE code exchange" state_has '"usercode":1,"poll":3,"code_exchange":1'
RT=$(sed -n 's/.*"refresh_token": *"\([^"]*\)".*/\1/p' "$AUTH" | head -1)
check "refresh grant (JSON, as codex sends it) returns 200" is "$(refresh_status "$RT")" 200
check "replay of the rotated refresh token is rejected (401)" is "$(refresh_status "$RT")" 401
codex logout >"$WORK/logout.out" 2>&1
check "logout clears the login" status_has 'Not logged in'
stop_stack

echo "== refresh: codex refreshes an expired access token and the mock rotates the refresh token"
start_stack refresh '{"expiresIn":-3600}'
login 30
check "login exits 0" is "$RC" 0
run_limited 25 codex exec --skip-git-repo-check hi >/dev/null 2>&1
check "codex called the refresh grant and got 200" state_has '"step":"refresh","status":200'
stop_stack

echo "== bad PKCE verifier: code exchange is rejected and nothing is stored"
start_stack pkce '{"corruptVerifier":true}'
login 30
check "login exits non-zero" bash -c "[ $RC != 0 ]"
check "error says device code exchange failed" out_has 'device code exchange failed'
check "mock rejected the exchange" state_has '"step":"code_exchange","status":400'
check "no credentials written" bash -c "[ ! -s '$HOME_DIR/.codex/auth.json' ]"
stop_stack

echo "== denied: CLI fails fast and stores nothing"
start_stack denied '{"mode":"denied"}'
login 30
check "login exits non-zero" bash -c "[ $RC != 0 ]"
check "error says device auth failed with status 400" out_has 'device auth failed with status 400'
check "no credentials written" bash -c "[ ! -s '$HOME_DIR/.codex/auth.json' ]"
stop_stack

echo "== pending-forever: CLI keeps polling until stopped"
start_stack pending '{"mode":"pending-forever","interval":1}'
login 8
check "login was still waiting when stopped" is "$RC" 124
check "no failure message" out_lacks 'failed'
check "mock saw repeated polls" state_has '"poll":([3-9]|[1-9][0-9])'
stop_stack

echo "== disabled: usercode 404"
start_stack disabled '{"disabled":true}'
login 20
check "CLI reports device code login is not enabled" out_has 'not enabled'
stop_stack

[ "$FAILED" = 0 ] && echo "ALL PASSED" || { echo "SOME CHECKS FAILED (logs in $WORK)"; trap - EXIT; cleanup; exit 1; }
