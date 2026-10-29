// Package postgres is a deterministic PostgreSQL v3 wire mock, not a SQL engine.
// Config: {"users":{"u":"p"},"database":"mock","unknownSelectError":false,
// "queries":[{"match":"^select 1$","columns":[{"name":"?column?","type":23}],
// "rows":[["1"]]}]}. Match is a case-insensitive Go regular expression applied
// to whitespace-normalized SQL. First rule wins; null cells are SQL NULL.
// Rules may set "error" to a five-character SQLSTATE (message is always generic).
// Supported result OIDs: bool 16, int8 20, int2 21, int4 23, text 25, varchar
// 1043, json 114, jsonb 3802, uuid 2950. Text and binary Bind formats are supported.
// Built-ins cover scalar SELECTs, version/current_database/current_schema, SET,
// SHOW, transaction tags, and lmclient's id/label network fixture tables and
// column metadata. Other writes acknowledge without executing. There is no TLS,
// SCRAM, real transaction isolation, cancellation, or general SQL evaluation.
package postgres

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgproto3/v2"
	"github.com/lmctl-ai/lmmock/internal/proto"
)

type column struct {
	Name string `json:"name"`
	Type uint32 `json:"type"`
}
type rule struct {
	Match   string      `json:"match"`
	Columns []column    `json:"columns"`
	Rows    [][]*string `json:"rows"`
	Error   string      `json:"error"`
	re      *regexp.Regexp
}
type config struct {
	Users              map[string]string `json:"users"`
	Database           string            `json:"database"`
	UnknownSelectError bool              `json:"unknownSelectError"`
	Queries            []rule            `json:"queries"`
}
type server struct {
	cfg    config
	mu     sync.Mutex
	tables map[string][][]*string
}
type result struct {
	columns []column
	rows    [][]*string
	tag     string
	code    string
}
type statement struct {
	sql   string
	types []uint32
}
type portal struct {
	result  result
	formats []int16
	offset  int
}

func init() { proto.Register("postgres", New) }
func New(raw json.RawMessage) (proto.Server, error) {
	s := &server{tables: map[string][][]*string{}}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &s.cfg); err != nil {
			return nil, proto.ConfigError("invalid postgres JSON")
		}
	}
	for i := range s.cfg.Queries {
		r := &s.cfg.Queries[i]
		var err error
		if r.Match == "" {
			return nil, proto.ConfigError("query match is required")
		}
		r.re, err = regexp.Compile("(?i)" + r.Match)
		if err != nil {
			return nil, proto.ConfigError("invalid query match")
		}
		if r.Error != "" && !regexp.MustCompile(`^[0-9A-Z]{5}$`).MatchString(r.Error) {
			return nil, proto.ConfigError("invalid SQLSTATE")
		}
		for j := range r.Columns {
			if r.Columns[j].Type == 0 {
				r.Columns[j].Type = 25
			}
			if !supported(r.Columns[j].Type) {
				return nil, proto.ConfigError("unsupported column type")
			}
		}
		for _, row := range r.Rows {
			if len(row) != len(r.Columns) {
				return nil, proto.ConfigError("row width differs from columns")
			}
			for j, v := range row {
				if _, err := encode(v, r.Columns[j].Type, 1); err != nil {
					return nil, proto.ConfigError("invalid typed cell")
				}
			}
		}
	}
	return s, nil
}
func (*server) Scheme() string { return "postgres" }
func (s *server) Serve(ctx context.Context, ln net.Listener) error {
	defer ln.Close()
	stop := context.AfterFunc(ctx, func() { ln.Close() })
	defer stop()
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer c.Close()
			stop := context.AfterFunc(ctx, func() { c.Close() })
			defer stop()
			s.connection(c)
		}()
	}
}
func (s *server) connection(c net.Conn) {
	c.SetDeadline(time.Now().Add(30 * time.Second))
	b := pgproto3.NewBackend(pgproto3.NewChunkReader(c), c)
	var startup *pgproto3.StartupMessage
	for startup == nil {
		m, err := b.ReceiveStartupMessage()
		if err != nil {
			return
		}
		switch m := m.(type) {
		case *pgproto3.SSLRequest, *pgproto3.GSSEncRequest:
			if _, err = c.Write([]byte("N")); err != nil {
				return
			}
		case *pgproto3.StartupMessage:
			startup = m
		default:
			return
		}
	}
	if s.cfg.Users != nil {
		b.SetAuthType(pgproto3.AuthTypeCleartextPassword)
		if b.Send(&pgproto3.AuthenticationCleartextPassword{}) != nil {
			return
		}
		msg, err := b.Receive()
		if err != nil {
			return
		}
		password, ok := msg.(*pgproto3.PasswordMessage)
		expected, exists := s.cfg.Users[startup.Parameters["user"]]
		if !ok || !exists || password.Password != expected {
			b.Send(&pgproto3.ErrorResponse{Severity: "FATAL", Code: "28P01", Message: "password authentication failed"})
			return
		}
	}
	b.Send(&pgproto3.AuthenticationOk{})
	for _, v := range [][2]string{{"server_version", "16.0"}, {"server_encoding", "UTF8"}, {"client_encoding", "UTF8"}, {"DateStyle", "ISO, MDY"}, {"TimeZone", "UTC"}, {"integer_datetimes", "on"}, {"standard_conforming_strings", "on"}} {
		b.Send(&pgproto3.ParameterStatus{Name: v[0], Value: v[1]})
	}
	b.Send(&pgproto3.BackendKeyData{ProcessID: 1, SecretKey: 1})
	b.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	database := startup.Parameters["database"]
	if database == "" {
		database = startup.Parameters["user"]
	}
	if s.cfg.Database != "" {
		database = s.cfg.Database
	}
	bound := map[string]boundQuery{}
	statements := map[string]statement{}
	portals := map[string]*portal{}
	failed := false
	tx := byte('I')
	fail := func(code string) {
		b.Send(&pgproto3.ErrorResponse{Severity: "ERROR", Code: code, Message: "mock query rejected"})
		failed = true
		if tx == 'T' {
			tx = 'E'
		}
	}
	for {
		c.SetDeadline(time.Now().Add(30 * time.Second))
		msg, err := b.Receive()
		if err != nil {
			return
		}
		if failed {
			switch msg.(type) {
			case *pgproto3.Sync, *pgproto3.Terminate:
			default:
				continue
			}
		}
		switch m := msg.(type) {
		case *pgproto3.Terminate:
			return
		case *pgproto3.Query:
			for _, sql := range splitSQL(m.String, ';') {
				if strings.TrimSpace(sql) == "" {
					continue
				}
				r := s.query(sql, nil, database, true)
				if r.code != "" {
					fail(r.code)
					break
				}
				if r.tag == "BEGIN" {
					tx = 'T'
				}
				if r.tag == "COMMIT" || r.tag == "ROLLBACK" {
					tx = 'I'
				}
				if len(r.columns) > 0 {
					b.Send(description(r.columns, nil))
				}
				if !sendRows(b, &portal{result: r}, 0) {
					return
				}
			}
			if strings.TrimSpace(m.String) == "" {
				b.Send(&pgproto3.EmptyQueryResponse{})
			}
			failed = false
			b.Send(&pgproto3.ReadyForQuery{TxStatus: tx})
		case *pgproto3.Parse:
			types := inferTypes(m.Query, m.ParameterOIDs)
			statements[m.Name] = statement{m.Query, types}
			b.Send(&pgproto3.ParseComplete{})
		case *pgproto3.Bind:
			st, ok := statements[m.PreparedStatement]
			if !ok {
				fail("26000")
				continue
			}
			if len(m.Parameters) != len(st.types) || !validFormats(m.ParameterFormatCodes, len(st.types)) {
				fail("08P01")
				continue
			}
			params := make([]*string, len(m.Parameters))
			good := true
			for i, v := range m.Parameters {
				var e error
				params[i], e = decode(v, st.types[i], format(m.ParameterFormatCodes, i))
				if e != nil {
					good = false
					break
				}
			}
			if !good {
				fail("22P02")
				continue
			}
			r := s.query(st.sql, params, database, false)
			if !validFormats(m.ResultFormatCodes, len(r.columns)) {
				fail("08P01")
				continue
			}
			// Writes are applied only on Execute, never on Bind/Describe.
			portals[m.DestinationPortal] = &portal{result: r, formats: append([]int16(nil), m.ResultFormatCodes...)}
			bound[m.DestinationPortal] = boundQuery{st.sql, params, database}
			b.Send(&pgproto3.BindComplete{})
		case *pgproto3.Describe:
			var r result
			var formats []int16
			if m.ObjectType == 'S' {
				st, ok := statements[m.Name]
				if !ok {
					fail("26000")
					continue
				}
				b.Send(&pgproto3.ParameterDescription{ParameterOIDs: st.types})
				r = s.query(st.sql, nil, database, false)
			} else {
				p, ok := portals[m.Name]
				if !ok {
					fail("34000")
					continue
				}
				r = p.result
				formats = p.formats
			}
			if len(r.columns) == 0 {
				b.Send(&pgproto3.NoData{})
			} else {
				b.Send(description(r.columns, formats))
			}
		case *pgproto3.Execute:
			p, ok := portals[m.Portal]
			if !ok {
				fail("34000")
				continue
			}
			if q, ok := bound[m.Portal]; ok {
				p.result = s.query(q.sql, q.params, q.database, true)
				delete(bound, m.Portal)
			}
			if p.result.code != "" {
				fail(p.result.code)
				continue
			}
			if p.result.tag == "BEGIN" {
				tx = 'T'
			}
			if p.result.tag == "COMMIT" || p.result.tag == "ROLLBACK" {
				tx = 'I'
			}
			if !sendRows(b, p, m.MaxRows) {
				return
			}
		case *pgproto3.Sync:
			failed = false
			b.Send(&pgproto3.ReadyForQuery{TxStatus: tx})
		case *pgproto3.Flush: // Send writes immediately.
		case *pgproto3.Close:
			if m.ObjectType == 'S' {
				delete(statements, m.Name)
			} else {
				delete(portals, m.Name)
				delete(bound, m.Name)
			}
			b.Send(&pgproto3.CloseComplete{})
		default:
			fail("0A000")
		}
	}
}

type boundQuery struct {
	sql      string
	params   []*string
	database string
}

func validFormats(f []int16, n int) bool {
	if len(f) > 1 && len(f) != n {
		return false
	}
	for _, v := range f {
		if v != 0 && v != 1 {
			return false
		}
	}
	return true
}
func format(f []int16, i int) int16 {
	if len(f) == 0 {
		return 0
	}
	if len(f) == 1 {
		return f[0]
	}
	return f[i]
}
func description(cols []column, f []int16) *pgproto3.RowDescription {
	r := &pgproto3.RowDescription{}
	for i, c := range cols {
		size := int16(-1)
		switch c.Type {
		case 16:
			size = 1
		case 21:
			size = 2
		case 23:
			size = 4
		case 20:
			size = 8
		case 2950:
			size = 16
		}
		r.Fields = append(r.Fields, pgproto3.FieldDescription{Name: []byte(c.Name), DataTypeOID: c.Type, DataTypeSize: size, TypeModifier: -1, Format: format(f, i)})
	}
	return r
}
func sendRows(b *pgproto3.Backend, p *portal, max uint32) bool {
	end := len(p.result.rows)
	if max > 0 && p.offset+int(max) < end {
		end = p.offset + int(max)
	}
	for p.offset < end {
		row := p.result.rows[p.offset]
		values := make([][]byte, len(row))
		for i, v := range row {
			var err error
			values[i], err = encode(v, p.result.columns[i].Type, format(p.formats, i))
			if err != nil {
				return false
			}
		}
		if b.Send(&pgproto3.DataRow{Values: values}) != nil {
			return false
		}
		p.offset++
	}
	if p.offset < len(p.result.rows) {
		return b.Send(&pgproto3.PortalSuspended{}) == nil
	}
	tag := p.result.tag
	if tag == "" {
		tag = fmt.Sprintf("SELECT %d", len(p.result.rows))
	}
	return b.Send(&pgproto3.CommandComplete{CommandTag: []byte(tag)}) == nil
}
func supported(t uint32) bool {
	switch t {
	case 16, 20, 21, 23, 25, 1043, 114, 3802, 2950:
		return true
	}
	return false
}
func encode(v *string, t uint32, f int16) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	if t == 16 {
		switch strings.ToLower(*v) {
		case "true", "t", "1":
			if f == 0 {
				return []byte("t"), nil
			}
			return []byte{1}, nil
		case "false", "f", "0":
			if f == 0 {
				return []byte("f"), nil
			}
			return []byte{0}, nil
		default:
			return nil, fmt.Errorf("bool")
		}
	}

	if f == 0 {
		return []byte(*v), nil
	}
	switch t {
	case 20, 21, 23:
		bits := 64
		if t == 21 {
			bits = 16
		}
		if t == 23 {
			bits = 32
		}
		n, e := strconv.ParseInt(*v, 10, bits)
		if e != nil {
			return nil, e
		}
		b := make([]byte, bits/8)
		switch bits {
		case 16:
			binary.BigEndian.PutUint16(b, uint16(n))
		case 32:
			binary.BigEndian.PutUint32(b, uint32(n))
		default:
			binary.BigEndian.PutUint64(b, uint64(n))
		}
		return b, nil
	case 2950:
		b, e := hex.DecodeString(strings.ReplaceAll(*v, "-", ""))
		if e != nil || len(b) != 16 {
			return nil, fmt.Errorf("uuid")
		}
		return b, nil
	case 3802:
		if !json.Valid([]byte(*v)) {
			return nil, fmt.Errorf("json")
		}
		return append([]byte{1}, []byte(*v)...), nil
	default:
		return []byte(*v), nil
	}
}
func decode(v []byte, t uint32, f int16) (*string, error) {
	if v == nil {
		return nil, nil
	}
	if f == 0 {
		return str(string(v)), nil
	}
	switch t {
	case 16:
		if len(v) != 1 {
			return nil, fmt.Errorf("bool")
		}
		if v[0] == 1 {
			return str("t"), nil
		}
		return str("f"), nil
	case 21:
		if len(v) != 2 {
			return nil, fmt.Errorf("int2")
		}
		return str(strconv.FormatInt(int64(int16(binary.BigEndian.Uint16(v))), 10)), nil
	case 23:
		if len(v) != 4 {
			return nil, fmt.Errorf("int4")
		}
		return str(strconv.FormatInt(int64(int32(binary.BigEndian.Uint32(v))), 10)), nil
	case 20:
		if len(v) != 8 {
			return nil, fmt.Errorf("int8")
		}
		return str(strconv.FormatInt(int64(binary.BigEndian.Uint64(v)), 10)), nil
	case 2950:
		if len(v) != 16 {
			return nil, fmt.Errorf("uuid")
		}
		h := hex.EncodeToString(v)
		return str(h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]), nil
	case 3802:
		if len(v) < 1 || v[0] != 1 {
			return nil, fmt.Errorf("jsonb")
		}
		return str(string(v[1:])), nil
	default:
		return str(string(v)), nil
	}
}
func str(s string) *string { return &s }

var paramRE = regexp.MustCompile(`\$(\d+)(?:::(\w+))?`)

func oid(s string) uint32 {
	switch strings.ToLower(s) {
	case "int", "integer", "int4":
		return 23
	case "bigint", "int8":
		return 20
	case "smallint", "int2":
		return 21
	case "bool", "boolean":
		return 16
	case "jsonb":
		return 3802
	case "json":
		return 114
	case "uuid":
		return 2950
	case "varchar":
		return 1043
	}
	return 25
}
func inferTypes(sql string, types []uint32) []uint32 {
	out := append([]uint32(nil), types...)
	for _, m := range paramRE.FindAllStringSubmatch(sql, -1) {
		n, _ := strconv.Atoi(m[1])
		if n > 65535 {
			continue
		}
		for len(out) < n {
			out = append(out, 0)
		}
		if n > 0 && out[n-1] == 0 {
			out[n-1] = oid(m[2])
		}
	}
	for i := range out {
		if out[i] == 0 {
			out[i] = 25
		}
	}
	return out
}

// Split only outside SQL single/double quotes. This is deliberately not a full parser.
func splitSQL(s string, sep byte) []string {
	var out []string
	start := 0
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				if i+1 < len(s) && s[i+1] == quote {
					i++
				} else {
					quote = 0
				}
			}
		} else if c == '\'' || c == '"' {
			quote = c
		} else if c == sep {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
