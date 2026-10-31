# M2 streaming, TLS and request observations

HTTP service policy stays in annotated lmcode Handler entries. The Go additions
are transport/lifecycle and generic state/observation primitives in streaming.go,
tls.go and request_log.go. New service routes do not require new Go dispatch code.

stream_event no longer queues until Handler completion. It commits headers, writes
bytes and flushes before returning to the VM. After commit, body/header/status/delay
replacement refuses. A later VM failure aborts the response rather than appending
HTTP 500. stream_fault chooses HTTP/1 disconnect, truncated-chunk or malformed-chunk;
it closes the hijacked connection after an optional valid prefix. No timers/pacing.
The existing buffered delay fixtures were not redesigned by this slice.

TLS is per listener/run, explicit --tls with optional repeated --tls-host identities.
A public CA file is exported to the exec child in the documented trust variables;
private keys remain in memory, no machine trust is installed, and Close removes
its private directory. Server.Close is idempotent. Fixed URL authority/SNI remains
unchanged when a client routes its connection to the loopback listener. The Go
assertion client loads LMMOCK_CA_CERT explicitly for platform-neutral trust.

Request logs are opt-in and redacted using the existing scenario scrubber.
Each arrival gets an immutable ID; snapshots sort by arrival even if handlers
finish out of order. Status/bytes/fault observations update during streaming;
complete=false distinguishes active requests. Bounded consumed-body logging is
separately opt-in and marks truncation/incomplete reads. Encoded/binary request
bodies are omitted rather than passed through a secret-obscuring base64 log.
Reserved admin routes are authored in scenario.lms/streaming.lms; their requests
are excluded from history. reset clears shared maps and advances a history epoch
so old active records cannot reappear. Quiesce requests for transactional test reset;
this does not stop a handler already running.

Host schema v4 adds generic stream_fault, request_log and reset methods. Frozen v3
and v1 schemas remain accepted to preserve already-compiled HTTP fixtures. Keep
embedded/examples schema bytes identical; compile new scripts against schema v4.
Service SSE splitting is ordinary lmcode in the provider presets, preserving bytes.

Test-first old-code evidence is the phase assertion [done done] instead of [one two].
The regular suite includes real fixed-HTTPS Node chunks, fresh-process untrusted-CA
refusal, partial framing errors, all-request observations, occurrence reset, and
Go overlay mutations that must fail semantic assertions after compiling.
