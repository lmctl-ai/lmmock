// Package mongodb provides an auth-less, in-memory MongoDB wire mock.
// Config: {"maxDocuments":10000}; the positive limit applies per collection.
// Unknown config fields and nonempty "users" are rejected: SCRAM is unsupported.
// Supports hello/isMaster, ping, insert, find, update ($set), delete, count,
// listCollections and listDatabases. Filters support equality and numeric $gt.
// Results use one exhausted cursor batch; sessions, transactions, indexes,
// aggregation, TLS, compression and durable storage are unsupported.
package mongodb

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lmctl-ai/lmmock/internal/proto"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func init() { proto.Register("mongodb", New) }

type server struct {
	mu   sync.Mutex
	data map[string][]bson.M
	max  int
	next uint64
}

func New(raw json.RawMessage) (proto.Server, error) {
	cfg := struct {
		MaxDocuments int               `json:"maxDocuments"`
		Users        map[string]string `json:"users"`
	}{MaxDocuments: 10000}
	if len(raw) > 0 {
		if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
			return nil, proto.ConfigError("expected config object")
		}
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if err := d.Decode(&cfg); err != nil {
			return nil, proto.ConfigError("%v", err)
		}
		if d.Decode(new(any)) != io.EOF {
			return nil, proto.ConfigError("expected one config object")
		}
	}
	if cfg.MaxDocuments < 1 || len(cfg.Users) > 0 {
		return nil, proto.ConfigError("maxDocuments must be positive; users/SCRAM unsupported")
	}
	return &server{data: map[string][]bson.M{}, max: cfg.MaxDocuments}, nil
}
func (*server) Scheme() string { return "mongodb" }
func (s *server) Serve(ctx context.Context, ln net.Listener) error {
	defer ln.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	go func() { <-ctx.Done(); ln.Close() }()
	defer wg.Wait()
	for {
		c, err := ln.Accept()
		if err != nil {
			wasCancelled := ctx.Err() != nil
			cancel()
			if wasCancelled {
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
			s.conn(c)
		}()
	}
}

const maxFrame = 16 * 1024 * 1024

var le = binary.LittleEndian

func doc(p []byte) (bson.D, int, error) {
	if len(p) < 4 {
		return nil, 0, io.ErrUnexpectedEOF
	}
	n := int(le.Uint32(p))
	if n < 5 || n > len(p) {
		return nil, 0, io.ErrUnexpectedEOF
	}
	var d bson.D
	err := bson.Unmarshal(p[:n], &d)
	return d, n, err
}
func parse(op uint32, p []byte) (bson.D, uint32, error) {
	if op == 2004 {
		if len(p) < 5 {
			return nil, 0, io.ErrUnexpectedEOF
		}
		z := bytes.IndexByte(p[4:], 0)
		if z < 0 || 4+z+1+8 > len(p) {
			return nil, 0, io.ErrUnexpectedEOF
		}
		d, _, e := doc(p[4+z+1+8:])
		return d, 0, e
	}
	if op != 2013 || len(p) < 5 {
		return nil, 0, fmt.Errorf("unsupported frame")
	}
	flags := le.Uint32(p)
	if flags&0xffff & ^uint32(2) != 0 {
		return nil, 0, fmt.Errorf("unsupported flags")
	}
	p = p[4:]
	var cmd bson.D
	sequences := bson.D{}
	for len(p) > 0 {
		kind := p[0]
		p = p[1:]
		switch kind {
		case 0:
			d, n, e := doc(p)
			if e != nil {
				return nil, 0, e
			}
			if cmd != nil {
				return nil, 0, fmt.Errorf("duplicate body")
			}
			cmd = d
			p = p[n:]
		case 1:
			if len(p) < 4 {
				return nil, 0, io.ErrUnexpectedEOF
			}
			n := int(le.Uint32(p))
			if n < 5 || n > len(p) {
				return nil, 0, io.ErrUnexpectedEOF
			}
			part := p[4:n]
			z := bytes.IndexByte(part, 0)
			if z < 0 {
				return nil, 0, io.ErrUnexpectedEOF
			}
			key := string(part[:z])
			part = part[z+1:]
			a := bson.A{}
			for len(part) > 0 {
				d, k, e := doc(part)
				if e != nil {
					return nil, 0, e
				}
				a = append(a, d)
				part = part[k:]
			}
			sequences = append(sequences, bson.E{Key: key, Value: a})
			p = p[n:]
		default:
			return nil, 0, fmt.Errorf("bad section")
		}
	}
	if len(cmd) == 0 {
		return nil, 0, fmt.Errorf("missing command")
	}
	return append(cmd, sequences...), flags, nil
}
func (s *server) conn(c net.Conn) {
	for {
		c.SetDeadline(time.Now().Add(30 * time.Second))
		h := make([]byte, 16)
		if _, e := io.ReadFull(c, h); e != nil {
			return
		}
		n := int(le.Uint32(h))
		if n < 16 || n > maxFrame {
			return
		}
		p := make([]byte, n-16)
		if _, e := io.ReadFull(c, p); e != nil {
			return
		}
		op := le.Uint32(h[12:])
		d, flags, e := parse(op, p)
		if e != nil {
			return
		}
		s.mu.Lock()
		reply := s.command(d)
		b, e := bson.Marshal(reply)
		s.mu.Unlock()
		if flags&2 != 0 {
			continue
		}
		if e != nil {
			return
		}
		prefix := 5
		outOp := uint32(2013)
		if op == 2004 {
			prefix = 20
			outOp = 1
		}
		out := make([]byte, 16+prefix+len(b))
		le.PutUint32(out, uint32(len(out)))
		le.PutUint32(out[4:], le.Uint32(h[4:])+1)
		copy(out[8:12], h[4:8])
		le.PutUint32(out[12:], outOp)
		if op == 2004 {
			le.PutUint32(out[32:], 1)
		}
		copy(out[16+prefix:], b)
		if _, e = c.Write(out); e != nil {
			return
		}
	}
}
func m(v any) bson.M {
	switch d := v.(type) {
	case bson.M:
		return d
	case bson.D:
		r := bson.M{}
		for _, e := range d {
			r[e.Key] = e.Value
		}
		return r
	}
	return bson.M{}
}
func num(v any) (float64, bool) {
	switch n := v.(type) {
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}
func equal(a, b any) bool {
	x, ok := num(a)
	y, ok2 := num(b)
	if ok && ok2 {
		return x == y
	}
	return reflect.DeepEqual(a, b)
}
func validate(f bson.M) error {
	for k, v := range f {
		if strings.HasPrefix(k, "$") {
			return fmt.Errorf("unknown top level operator: %s", k)
		}
		for op, x := range m(v) {
			if strings.HasPrefix(op, "$") {
				if op != "$gt" {
					return fmt.Errorf("unsupported operator: %s", op)
				}
				if _, ok := num(x); !ok {
					return fmt.Errorf("$gt requires numeric operand")
				}
			}
		}
	}
	return nil
}
func matches(d, f bson.M) bool {
	for k, v := range f {
		if gt, ok := m(v)["$gt"]; ok {
			x, yes := num(d[k])
			y, _ := num(gt)
			if !yes || x <= y {
				return false
			}
		} else if !equal(d[k], v) {
			return false
		}
	}
	return true
}
func fail(code int32, msg string) bson.M {
	return bson.M{"ok": 0.0, "code": code, "codeName": "BadValue", "errmsg": msg}
}
func cursor(ns string, a bson.A) bson.M {
	return bson.M{"ok": 1.0, "cursor": bson.M{"id": int64(0), "ns": ns, "firstBatch": a}}
}
func (s *server) command(d bson.D) bson.M {
	if len(d) == 0 {
		return fail(2, "empty command")
	}
	c := m(d)
	name := strings.ToLower(d[0].Key)
	db, _ := c["$db"].(string)
	coll, _ := d[0].Value.(string)
	ns := db + "." + coll
	switch name {
	case "hello", "ismaster":
		return bson.M{"ok": 1.0, "ismaster": true, "isWritablePrimary": true, "helloOk": true, "minWireVersion": int32(0), "maxWireVersion": int32(17), "maxBsonObjectSize": int32(maxFrame), "maxMessageSizeBytes": int32(maxFrame), "maxWriteBatchSize": int32(1000), "connectionId": int32(1)}
	case "ping", "endsessions":
		return bson.M{"ok": 1.0}
	case "insert":
		a, _ := c["documents"].(bson.A)
		if len(s.data[ns])+len(a) > s.max {
			return fail(2, "collection document limit exceeded")
		}
		for _, v := range a {
			item := m(v)
			if _, ok := item["_id"]; !ok {
				s.next++
				item["_id"] = fmt.Sprintf("mock-%d", s.next)
			}
			for _, old := range s.data[ns] {
				if equal(old["_id"], item["_id"]) {
					return fail(11000, "duplicate _id")
				}
			}
			s.data[ns] = append(s.data[ns], item)
		}
		return bson.M{"ok": 1.0, "n": int32(len(a))}
	case "find", "count":
		f := m(c["filter"])
		if name == "count" {
			f = m(c["query"])
		}
		if e := validate(f); e != nil {
			return fail(2, e.Error())
		}
		a := bson.A{}
		limit, _ := num(c["limit"])
		if limit < 0 {
			limit = -limit
		}
		for _, item := range s.data[ns] {
			if matches(item, f) {
				a = append(a, item)
				if limit > 0 && len(a) >= int(limit) {
					break
				}
			}
		}
		if name == "count" {
			return bson.M{"ok": 1.0, "n": int32(len(a))}
		}
		return cursor(ns, a)
	case "delete", "update":
		key := "deletes"
		if name == "update" {
			key = "updates"
		}
		a, _ := c[key].(bson.A)
		n := int32(0)
		for _, v := range a {
			spec := m(v)
			f := m(spec["q"])
			if e := validate(f); e != nil {
				return fail(2, e.Error())
			}
			set := bson.M{}
			if name == "update" {
				u := m(spec["u"])
				if len(u) != 1 || u["$set"] == nil || spec["upsert"] == true {
					return fail(2, "only $set without upsert is supported")
				}
				set = m(u["$set"])
				if _, ok := set["_id"]; ok {
					return fail(2, "cannot update _id")
				}
			}
			remaining := []bson.M{}
			matched := false
			limit, _ := num(spec["limit"])
			for _, item := range s.data[ns] {
				hit := matches(item, f) && (!matched || (name == "delete" && limit == 0) || (name == "update" && spec["multi"] == true))
				if hit {
					matched = true
					n++
					if name == "delete" {
						continue
					}
					for k, x := range set {
						item[k] = x
					}
				}
				remaining = append(remaining, item)
			}
			s.data[ns] = remaining
		}
		return bson.M{"ok": 1.0, "n": n, "nModified": n}
	case "listcollections":
		a := bson.A{}
		keys := []string{}
		for k := range s.data {
			if strings.HasPrefix(k, db+".") {
				keys = append(keys, strings.TrimPrefix(k, db+"."))
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			a = append(a, bson.M{"name": k, "type": "collection", "options": bson.M{}})
		}
		return cursor(db+".$cmd.listCollections", a)
	case "listdatabases":
		seen := map[string]bool{}
		for k := range s.data {
			seen[strings.SplitN(k, ".", 2)[0]] = true
		}
		keys := []string{}
		for k := range seen {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		a := bson.A{}
		for _, k := range keys {
			a = append(a, bson.M{"name": k, "sizeOnDisk": int64(0), "empty": false})
		}
		return bson.M{"ok": 1.0, "databases": a, "totalSize": int64(0)}
	default:
		return fail(59, "unsupported command: "+d[0].Key)
	}
}
