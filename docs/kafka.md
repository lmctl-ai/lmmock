# Kafka fixture

`lmmock proto kafka --ready-file ready.json` serves one in-memory plaintext
broker. Use the readiness `address` (host:port), not the `kafka://` URL, as
`LMCLIENT_KAFKA_URL` or `lmclient kafka ... --url`.

Optional configuration: `{"topics":{"events":1,"audit":2}}`. Topic names follow
Kafka's ASCII name rules; counts are 1–100, with at most 100 topics and 1000
seeded partitions. Unknown fields fail. Missing topics auto-create with one
partition. All data disappears on shutdown; reads replay rather than removing
records. The broker binds only the supplied loopback listener and advertises
that same endpoint, with no internal listener or proxy.

The pinned BSD-3-Clause franz-go kfake revision is
`123ec885bc9766fe08389f7c1b29d652be12ece8` (Go 1.26.0 minimum). Its `ListenFn`
option solves the listener integration concern in the earlier feasibility note.
The adapter closes active sockets on cancellation and renews a one-minute I/O
deadline on reads/writes. Tests cover shutdown while a partial frame is pending.

The supported lmclient contract is listTopics/ping, partition-zero produce with
acks=all and default producer idempotence, explicit assignment and earliest reads,
UTF-8/empty values, ordered replay, and partial results at the client's deadline.
This fixture is not a replication, KRaft, durability, retention, authorization,
TLS/SASL, transactions, or consumer-group fidelity test. kfake may implement
additional operations, but this adapter does not promise their production parity.

## Reproduce verification

The standard `lmscript --no-preflight test.lms` includes Go network round trips
using franz-go. `go test -race ./internal/proto/kafka` additionally checks races.
For the actual Java kafka-clients 3.9.1 driver, build a current lmclient jar with
`mvn -q -B package -DskipTests` in its checkout, then run:

```sh
python3 tests/kafka-java.py build/lmmock /path/to/lmclient/target/lmclient-0.2.0-all.jar
```

The optional harness owns the mock process and exercises ten real lmclient calls:
ping through env and flag endpoints, three separate idempotent producers, ordered
consumption, earliest-offset replay, a partial result at 1500 ms, and an empty
value round trip. It requires Java 21+ and no Docker. It is separate from the
standard Go/Node gate because lmmock does not bundle lmclient or a JDK.

lmclient's existing `scripts/test-kafka.py` still starts Docker; converting that
suite to an external endpoint is deferred to its repository. Local Linux tests
and cross-compilation do not establish macOS/Windows runtime verification; those
platforms must execute the standard suite in CI.
