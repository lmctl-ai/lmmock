# PostgreSQL fixture

Run `lmmock proto postgres --config pg.json --ready-file ready.json`. The URL
uses `postgres://`; JDBC callers prefix the address with `jdbc:postgresql://`
and append `/fixture`. No Docker or external database is needed.

```json
{
  "users": {"u": "dummy-pass"},
  "database": "fixture",
  "unknownSelectError": true,
  "queries": [
    {"match":"^select custom$", "columns":[{"name":"n","type":23}], "rows":[["42"]]},
    {"match":"^select denied$", "error":"42501"}
  ]
}
```

Omit `users` for AuthenticationOk without a challenge; an empty users map denies
everyone. Configured users require a cleartext password (loopback fixture only).
SSL/GSS encryption requests receive `N`. Config query rules precede built-ins;
matching is case-insensitive regex on whitespace-normalized SQL, first match
wins. Quote contents are preserved. Cells are strings or null; OID defaults to
text (25). Supported OIDs: bool 16, int2 21, int4 23, int8 20, text 25, varchar
1043, json 114, jsonb 3802, uuid 2950. Binary parameters/results are supported for
these types. Invalid configs produce generic ConfigError messages without values.
`unknownSelectError` defaults false (empty result); true returns SQLSTATE 0A000.

## Verified client contract

The lmclient network suite issues SELECT 1, CREATE TABLE/INSERT for an id/label
fixture, SELECT id,label, SELECT label, JDBC column metadata, EXPLAIN, JSONB/UUID
casts, and DROP TABLE. Fixture tables persist across connections until dropped.
Only this two-column table shape is implemented; it is not general SQL storage.

Captured PostgreSQL JDBC 42.7.8 startup/catalog requests include:

- `SET application_name = 'PostgreSQL JDBC Driver'`
- `select current_catalog`
- `SELECT n.nspname = ANY(current_schemas(true)), n.nspname, t.typname FROM
  pg_catalog.pg_type t JOIN pg_catalog.pg_namespace n ON t.typnamespace = n.oid
  WHERE t.oid = $1`
- A prepared pg_namespace/pg_class/pg_attribute/pg_type join for getColumns,
  with `c.relname LIKE $1` and `attname LIKE $2` (the mock supports the fixture
  table name and `%` column selection, not general SQL LIKE evaluation).

JDBC SELECT version/current_database/current_schema, pgx simple queries, named
and unnamed Parse/Bind/Describe/Execute/Sync, SQLSTATE failures and recovery,
text/binary values, and eight repeated JDBC prepared executions are covered.
No statement/parameter logging is enabled in the shipped mock.

```sh
go test -v ./internal/proto/postgres
POSTGRES_JDBC_JAR=/path/to/postgresql-42.7.8.jar go test -v ./internal/proto/postgres
```

The optional JDBC probe needs Java 21 and a locally provided driver jar; normal
CI tests use pgx and only loopback. Local lmclient `scripts/test-network.py`
completed with `postgres: PASS` (SQL Server was unconfigured and skipped).
Example real client output: `SELECT id,label FROM fixture` returned
`[{"id":1,"label":"network-ok"}]`; JSONB/UUID and column metadata also passed.

## Deliberate limitations

This is a deterministic fixture, not PostgreSQL. No TLS, SCRAM, CancelRequest,
replication, COPY, transactions/rollback semantics, SQL planning, constraints,
full catalog, schema filtering, full type registry, or arbitrary SQL evaluation.
Transaction status/tags are simulated; writes outside the id/label fixture only
return plausible command tags. Configured rules return canned rows independent
of bound values. Unmatched SELECTs return an empty text-column result unless
configured to error. Every connection has a 30-second I/O deadline and context
cancellation closes the listener and active connections on every platform.
