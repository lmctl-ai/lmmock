# MongoDB mock

Run `lmmock proto mongodb --listen 127.0.0.1:0`; use the readiness JSON's URL
as `LMCLIENT_MONGO_URL` for lmclient's scripts/test-mongo.py. No Docker is needed.
The pinned specification is MongoArea.java and test-mongo.py (Java sync driver
5.2.1). The live suite exercises ping, ObjectId/custom-ID insert, array preservation,
find/limit/equality/numeric $gt, invalid-operator errors and deleteMany.

Config is an optional JSON object: `{"maxDocuments":10000}` limits each collection.
The limit must be positive. Unknown keys, malformed config and nonempty `users`
are config errors. No credentials are needed. SCRAM/authentication is deferred;
the current live suite is auth-less. Never use actual secrets with this mock.

Legacy OP_QUERY command handshakes and OP_MSG body/document-sequence sections are
supported. Handshakes advertise a standalone writable server, wire version 17,
no sessions and no compression. Replies are bounded by the small-fixture use case;
find returns one exhausted cursor batch and ignores batchSize. Large collections
requiring cursor pagination are out of scope. Incoming frames have a 16 MiB bound
and connections a 30-second deadline, with immediate close on context cancellation.

Additional commands: count (not aggregate-based countDocuments), $set-only updates
without upsert, listCollections and listDatabases. Equality is on top-level BSON
values; numeric $gt is supported. Dot paths, other query operators, projections,
sorting and skip are unsupported. Updates report matched rows as nModified even
for identical values. Missing raw-wire IDs receive deterministic mock-N strings;
official clients generate their own ObjectIds. Duplicate IDs produce a command
error rather than MongoDB's per-write writeErrors array; ordered bulk partial
success is not modeled precisely. State survives connections, not process exits.
Transactions, TLS, compression, aggregation, indexes and replication are deferred.

The official MongoDB Go BSON codec v2.4.0 handles nested BSON and ObjectIds. Only
bson is imported in production; the full official client is a test dependency.
This is less error-prone than a partial BSON reimplementation and Apache-2.0 is
compatible with this repository's dependency policy. Wire format reference:
[MongoDB specification](https://www.mongodb.com/docs/manual/reference/mongodb-wire-protocol/).

Local validation:

```
go test -v ./internal/proto/mongodb
--- PASS: TestDriverRoundTrip
--- PASS: TestLegacyHello
--- PASS: TestConfigAndMalformed
PASS
lmclient mongo checks: 27 passed (JVM; live via LMCLIENT_MONGO_URL)
```

Manual Java lmclient session (same loopback mock):

```
mongo ping --url mongodb://127.0.0.1:39629
{"ok":true,"result":{"pong":true}}
mongo insert --url mongodb://127.0.0.1:39629 --db demo --collection items --doc '{"_id":"demo-1","name":"coffee","qty":7}'
{"ok":true,"result":{"insertedId":"demo-1"}}
mongo find --url mongodb://127.0.0.1:39629 --db demo --collection items --filter '{"qty":{"$gt":5}}'
{"ok":true,"result":{"count":1,"docs":[{"name":"coffee","qty":7,"_id":"demo-1"}]}}
```

Validation was on Linux. Cross-platform runtime validation remains the repository
CI's job; no claim of a completed Windows/macOS run is made here.
