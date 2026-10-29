# Kafka: IMPLEMENT

Reviewed lmclient scripts/test-kafka.py, KafkaArea.java and docs/areas/kafka.md.
The pinned Java client is kafka-clients 3.9.1. Ping calls listTopics; produce
writes partition zero with acks=all; consume discovers partitions, assigns them
explicitly and seeks to the beginning on each invocation. Tests require ordered
UTF-8 messages, empty values, replay and partial results at a deadline. There
are no group subscriptions or commits. The producer does not disable default
idempotence, so account for InitProducerId rather than assuming four RPCs suffice.

Recommend the BSD-3-Clause [franz-go kfake server](https://github.com/twmb/franz-go/blob/master/docs/testing.md),
which provides an in-memory wire-speaking cluster. Check its selected revision's
Go requirement, pin it, and test the Java client before adoption. Adapt listener
creation to the supplied loopback listener and verify shutdown on Windows.
Estimated effort: 2–4 engineering days for integration, Java round trips, error
knobs and cross-platform checks. This is an estimate, not tested interoperability.

A hand-written alternative is IMPLEMENT-MINIMAL: ApiVersions, Metadata with the
actual bound endpoint, InitProducerId, Produce, ListOffsets and Fetch, pinned to
compatible versions. It needs record batch lengths, CRC32C, offsets, correlation
IDs, bounded long polls and advertised feature discipline. Budget 1–2 weeks.
No replication, KRaft, transactions, retention or group rebalance fidelity is
needed for these checks. Do not claim those features from a canned broker.

The current Python suite unconditionally starts Docker; add an external endpoint
mode before using this mock in that suite. No Kafka implementation is added here.
