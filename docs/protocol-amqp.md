# AMQP 0-9-1 mock

`lmmock proto amqp --ready-file ready.json` reports an `amqp://127.0.0.1:PORT`
endpoint. For lmclient, use that endpoint with a trailing `/` for the default
virtual host. `--config config.json` accepts `{"users":{"u":"p"}}`; an omitted
or empty map permits any well-formed PLAIN credentials. A configured map rejects
incorrect pairs with connection.close ACCESS_REFUSED (403). Errors never echo
credentials. All clients and listeners close on context cancellation.

The wire subset covers protocol negotiation, connection start/tune/open/close,
channel open/close, queue.declare, confirm.select, basic.publish and basic.get.
Messages assemble across body frames and enter the FIFO before publisher ack.
An interrupted partial publish is discarded; completed messages survive client
connections. Basic properties are preserved. Get with noAck=true removes a
message and reports the remaining count; an empty queue returns basic.get-empty.
Heartbeat frames are accepted and sent at the negotiated interval.

Limits: 128 connections, 128 channels per connection, 128 KiB negotiated frame
maximum, 8 MiB message bodies, 1024 queues, 10000 queued messages, and 32 MiB total
queued/in-flight body and property bytes. Connection setup has 10-second read
deadlines; writes have 5-second deadlines. Without heartbeats, idle reads have
a 60-second deadline. Only loopback TCP listeners are allowed.

## Deliberate differences

- Durable queues and persistent messages remain in memory only; shutdown loses them.
- Virtual-host names isolate queues but do not require provisioning or per-host ACLs.
- Expiration properties are preserved without enforcing TTL.
- An unknown routing key returns NOT_FOUND instead of silently dropping a publish.
- Non-default exchanges, consumer subscriptions, manual ack/requeue, transactions,
  TLS, exclusive/auto-delete queues, and queue arguments are unsupported.

## StrangeQ spike

Inspected `maxpert/strangeq` commit
`b8e0517cc3007d237bedaa6be01092796c022eee` before implementing this subset.
Its root license is MIT, and the source includes confirm/get/auth handling.
However, `Server.Start` and `StartWithQuitChannel` create their own listeners;
there is no direct supplied-listener serving API in those entry points.

The decisive portability gate failed:

```text
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./server
server/alarm_monitor.go:394:19: undefined: syscall.Statfs_t
server/alarm_monitor.go:395:20: undefined: syscall.Statfs
server/system_metrics.go:98:19: undefined: syscall.Statfs_t
server/system_metrics.go:99:20: undefined: syscall.Statfs
```

The spike stopped at that failure, as required. No StrangeQ code or dependencies
were incorporated; further dependency-license and shutdown audits were unnecessary.
The hand-written server uses Go standard library packages only. Tests use the
BSD-2-Clause `rabbitmq/amqp091-go` client.

## Verification

`go test ./internal/proto/amqp` exercises the real Go client plus raw frames:
FIFO across connections, fragmented and empty bodies, properties, confirmation
ordering, partial-publish cleanup, authentication, channel errors, virtual hosts,
heartbeats, size bounds and portable context shutdown.

The real RabbitMQ Java 5.22.0 client was also exercised through a freshly built
lmclient JVM jar: all 18 assertions in `scripts/test-queue.py` passed with only
its Docker setup replaced by this mock (configured with guest/guest). No Docker
was used and no lmclient source files were changed. This includes three persistent
UTF-8 publishes, confirmed completion, ordered destructive reads, empty polling,
credentials-file success and bad-password failure.
