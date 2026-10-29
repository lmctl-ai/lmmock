package postgres

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func start(t *testing.T, cfg string) string {
	t.Helper()
	s, e := New(json.RawMessage(cfg))
	if e != nil {
		t.Fatal(e)
	}
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(2 * time.Second):
			t.Error("server did not stop")
		}
	})
	return ln.Addr().String()
}
func connect(t *testing.T, address, password string) *pgx.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, e := pgx.Connect(ctx, "postgres://u:"+password+"@"+address+"/fixture?sslmode=prefer")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close(context.Background()) })
	return c
}
func TestPGX(t *testing.T) {
	address := start(t, `{"users":{"u":"dummy-pass"}}`)
	c := connect(t, address, "dummy-pass")
	ctx := context.Background()
	var one int32
	if e := c.QueryRow(ctx, "select 1", pgx.QueryExecModeSimpleProtocol).Scan(&one); e != nil || one != 1 {
		t.Fatalf("simple: %d %v", one, e)
	}
	var version, db, schema string
	if e := c.QueryRow(ctx, "select version(), current_database(), current_schema()").Scan(&version, &db, &schema); e != nil || !strings.Contains(version, "PostgreSQL") || db != "fixture" || schema != "public" {
		t.Fatalf("probes: %q %q %q %v", version, db, schema, e)
	}
	if _, e := c.Prepare(ctx, "prepared", "select $1::int4 as n, $2::text as label, $3::bool as yes, $4::int8 as big"); e != nil {
		t.Fatal(e)
	}
	for _, mode := range []pgx.QueryExecMode{pgx.QueryExecModeCacheStatement, pgx.QueryExecModeDescribeExec} {
		var n int32
		var label string
		var yes bool
		var big int64
		e := c.QueryRow(ctx, "select $1::int4 as n, $2::text as label, $3::bool as yes, $4::int8 as big", mode, int32(42), "héllo; 'query'", true, int64(9000000000)).Scan(&n, &label, &yes, &big)
		if e != nil || n != 42 || label != "héllo; 'query'" || !yes || big != 9000000000 {
			t.Fatalf("prepared: %d %q %t %d %v", n, label, yes, big, e)
		}
	}
	var n int32
	var label string
	var yes bool
	var big int64
	if e := c.QueryRow(ctx, "prepared", int32(7), "named", false, int64(9)).Scan(&n, &label, &yes, &big); e != nil || n != 7 || label != "named" || yes || big != 9 {
		t.Fatalf("named: %v", e)
	}
	if _, e := c.Exec(ctx, "CREATE TABLE fixture (id INTEGER PRIMARY KEY, label VARCHAR(40)); INSERT INTO fixture VALUES (1, 'network-ok');"); e != nil {
		t.Fatal(e)
	}
	if e := c.QueryRow(ctx, "SELECT id,label FROM fixture").Scan(&n, &label); e != nil || n != 1 || label != "network-ok" {
		t.Fatalf("table: %v", e)
	}
	if _, e := c.Exec(ctx, "INSERT INTO fixture VALUES ($1::int4,$2::text)", int32(2), "second"); e != nil {
		t.Fatal(e)
	}
	rows, e := c.Query(ctx, "SELECT label FROM fixture")
	if e != nil {
		t.Fatal(e)
	}
	values, e := pgx.CollectRows(rows, pgx.RowTo[string])
	if e != nil || len(values) != 2 || values[1] != "second" {
		t.Fatalf("rows: %v %v", values, e)
	}
}
func TestAuthAndErrors(t *testing.T) {
	a := start(t, `{"users":{"u":"dummy-pass"},"unknownSelectError":true,"queries":[{"match":"^select fail$","error":"42501"}]}`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, e := pgx.Connect(ctx, "postgres://u:wrong@"+a+"/fixture?sslmode=disable")
	if c != nil {
		c.Close(ctx)
	}
	var pe *pgconn.PgError
	if !errors.As(e, &pe) || pe.Code != "28P01" {
		t.Fatalf("auth: %v", e)
	}
	c = connect(t, a, "dummy-pass")
	for _, mode := range []pgx.QueryExecMode{pgx.QueryExecModeSimpleProtocol, pgx.QueryExecModeCacheStatement} {
		var v string
		e = c.QueryRow(ctx, "select fail", mode).Scan(&v)
		if !errors.As(e, &pe) || pe.Code != "42501" {
			t.Fatalf("error: %v", e)
		}
		var n int
		if e = c.QueryRow(ctx, "select 1").Scan(&n); e != nil || n != 1 {
			t.Fatalf("sync recovery: %v", e)
		}
	}
	e = c.QueryRow(ctx, "select unsupported_expression").Scan(new(string))
	if !errors.As(e, &pe) || pe.Code != "0A000" {
		t.Fatalf("unmatched: %v", e)
	}
}
func TestConfigAndNull(t *testing.T) {
	for _, raw := range []string{`{`, `{"queries":[{"match":"["}]}`, `{"queries":[{"match":"x","columns":[{"name":"n","type":23}],"rows":[["no"]]}]}`, `{"queries":[{"match":"x","rows":[["extra"]]}]}`} {
		if _, e := New(json.RawMessage(raw)); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	c := connect(t, start(t, `{"queries":[{"match":"^select canned$","columns":[{"name":"n","type":23},{"name":"empty","type":25}],"rows":[["9",null]]}]}`), "")
	var n int32
	var empty *string
	if e := c.QueryRow(context.Background(), "select canned").Scan(&n, &empty); e != nil || n != 9 || empty != nil {
		t.Fatalf("canned: %v", e)
	}
}
func TestGSSRefusalAndShutdown(t *testing.T) {
	a := start(t, "")
	c, e := net.Dial("tcp", a)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	b := make([]byte, 8)
	binary.BigEndian.PutUint32(b, 8)
	binary.BigEndian.PutUint32(b[4:], 80877104)
	if _, e = c.Write(b); e != nil {
		t.Fatal(e)
	}
	r := make([]byte, 1)
	if _, e = io.ReadFull(c, r); e != nil || r[0] != 'N' {
		t.Fatalf("GSS refusal %q %v", r, e)
	}
}

func TestJDBC(t *testing.T) {
	jar := os.Getenv("POSTGRES_JDBC_JAR")
	if jar == "" {
		t.Skip("set POSTGRES_JDBC_JAR for optional real JDBC oracle")
	}
	address := start(t, `{"users":{"u":"dummy-pass"}}`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "java", "--class-path", jar, "testdata/JdbcProbe.java", address).CombinedOutput()
	if err != nil {
		t.Fatalf("JDBC oracle: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func TestQuotedSQL(t *testing.T) {
	c := connect(t, start(t, ""), "")
	var value string
	if err := c.QueryRow(context.Background(), "SELECT 'two  spaces; and ''quotes'''", pgx.QueryExecModeSimpleProtocol).Scan(&value); err != nil || value != "two  spaces; and 'quotes'" {
		t.Fatalf("quoted literal: %q %v", value, err)
	}
}
