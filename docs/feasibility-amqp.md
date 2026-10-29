# AMQP 0-9-1: IMPLEMENT-MINIMAL

Reviewed scripts/test-queue.py, QueueArea.java and docs/areas/queue.md. The client
is RabbitMQ Java 5.22.0. Tests require connection/channel creation, durable queue
declaration, three persistent UTF-8 publishes, publisher confirmations, ordered
destructive reads, empty-queue polling, credentials-file success and bad-password
failure. Crucially, reads use basic.get(autoAck=true), not basic.consume; publish
uses confirm.select and waits for basic.ack. A deliver-only mock fails this suite.

Implement protocol-header negotiation, connection.start/start-ok (PLAIN dummy
credentials), tune/tune-ok, open/open-ok, channel.open/open-ok, queue.declare,
confirm.select, basic.publish plus content header/body reassembly, publisher
basic.ack, basic.get/get-ok/get-empty, and connection/channel close handshakes.
Bound frame lengths, handle heartbeat frames, preserve FIFO state across CLI
connections, and acknowledge publishes only after storing the complete body.
A memory-only implementation can accept durable/persistent flags without disk
persistence, explicitly documenting that divergence. Estimate 4–7 engineering
days including real Java validation and portable shutdown tests.

Library review: [rabbitmq/amqp091-go](https://github.com/rabbitmq/amqp091-go)
is a BSD-2-Clause client, not a server. Wabbit's amqptest API uses its own mock
connection interfaces and is not evidence of Java TCP interoperability.
[StrangeQ](https://github.com/maxpert/strangeq) advertises an embeddable Go AMQP
0-9-1 broker and has an [MIT license](https://github.com/maxpert/strangeq/blob/main/LICENSE).
It is a plausible alternative for a 1–2 day spike: verify listener injection,
confirm/get/auth support, dependency licenses and CGO-free builds before choosing
it over the small hand-written subset. No library has been integration-tested
in this verdict. No AMQP code is added in this wave.

TLS, exchanges beyond the default, consumer subscriptions, requeue, transactions,
quorum queues and persistence are deferred. The Python test currently owns a
Docker container unconditionally and needs an external endpoint mode.
