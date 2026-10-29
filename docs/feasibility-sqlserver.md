# SQL Server / TDS: HOLD

Reviewed lmclient scripts/test-network.py and the SQL Server driver registration.
There is no standalone test-sqlserver.py. The network suite requires SELECT 1,
CREATE TABLE plus INSERT in one batch, SELECT id/label as CSV, SELECT label as
JSON, JDBC column metadata, explain, XML values and DROP TABLE. A canned SELECT 1
reply would not satisfy this test; table names contain a random suffix.

Minimum credible scope is TDS packet fragmentation/reassembly, PRELOGIN encryption
negotiation, LOGIN7 and login tokens (LOGINACK/ENVCHANGE/DONE), SQLBatch, RPC as
needed by JDBC metadata, COLMETADATA/ROW/DONE/ERROR tokens with correct types,
attention/cancel handling and connection reset behavior. Script recognition must
track the generated table and answer JDBC metadata procedures and explain probes.
XML requires its own type encoding. Capture the actual pinned JDBC driver's
startup queries and metadata RPCs before estimating a fixed response catalogue.

[Microsoft PRELOGIN state documentation](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-tds/cc823ca8-4867-4387-819d-cc5c19da5732)
and [LOGIN7 format](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-tds/773a62b6-ee89-4c02-9e5e-344882630aac)
show why negotiation/authentication alone is substantial. When encryption is
negotiated, TLS handshaking is encapsulated in TDS; merely adding a TLS listener
is insufficient. A plaintext-only mock would also constrain the supplied JDBC URL.

No suitable small permissively licensed Go server library was established in this
review. Hand-rolled, pinned-driver smoke fidelity might take 2–4 weeks plus a
1–2 day capture spike; general SQL Server fidelity is outside this mock's scope.
These are estimates, not interoperability claims. HOLD until the test contract
can be narrowed or a reusable server implementation is validated. No TDS code
is added; native/Windows behavior and authenticated/encrypted sessions remain
unverified.
