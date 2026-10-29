package mongodb

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func start(t *testing.T) (string, context.Context) {
	t.Helper()
	s, e := New(nil)
	if e != nil {
		t.Fatal(e)
	}
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(time.Second):
			t.Error("shutdown hung")
		}
	})
	return ln.Addr().String(), ctx
}
func TestDriverRoundTrip(t *testing.T) {
	addr, ctx := start(t)
	c, e := mongo.Connect(options.Client().ApplyURI("mongodb://" + addr).SetServerSelectionTimeout(2 * time.Second))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Disconnect(ctx)
	if e = c.Ping(ctx, nil); e != nil {
		t.Fatal(e)
	}
	col := c.Database("test").Collection("items")
	if _, e = col.InsertMany(ctx, []any{bson.M{"name": "tea", "qty": 3, "tags": bson.A{"hot", "green"}}, bson.M{"_id": "coffee", "name": "coffee", "qty": 7}}); e != nil {
		t.Fatal(e)
	}
	var found bson.M
	if e = col.FindOne(ctx, bson.M{"qty": bson.M{"$gt": 5}}).Decode(&found); e != nil || found["name"] != "coffee" {
		t.Fatalf("find: %v %v", found, e)
	}
	r, e := col.UpdateOne(ctx, bson.M{"_id": "coffee"}, bson.M{"$set": bson.M{"qty": 9}})
	if e != nil || r.MatchedCount != 1 {
		t.Fatalf("update: %v %v", r, e)
	}
	if e = col.FindOne(ctx, bson.M{"_id": "coffee"}).Decode(&found); e != nil || found["qty"] != int32(9) {
		t.Fatalf("updated doc: %v %v", found, e)
	}
	var count bson.M
	if e = c.Database("test").RunCommand(ctx, bson.D{{Key: "count", Value: "items"}}).Decode(&count); e != nil || count["n"] != int32(2) {
		t.Fatalf("count: %v %v", count, e)
	}
	names, e := c.Database("test").ListCollectionNames(ctx, bson.D{})
	if e != nil || len(names) != 1 || names[0] != "items" {
		t.Fatalf("collections: %v %v", names, e)
	}
	if _, e = col.Find(ctx, bson.M{"$bogus": 1}); e == nil {
		t.Fatal("invalid operator accepted")
	}
	deleted, e := col.DeleteMany(ctx, bson.M{"name": "tea"})
	if e != nil || deleted.DeletedCount != 1 {
		t.Fatalf("delete: %v %v", deleted, e)
	}
	cur, e := col.Find(ctx, bson.D{})
	if e != nil {
		t.Fatal(e)
	}
	var all []bson.M
	if e = cur.All(ctx, &all); e != nil || len(all) != 1 {
		t.Fatalf("remaining: %v %v", all, e)
	}
	t.Log("official Go driver: ping, insertMany, $gt find, $set update, count, listCollections, invalid filter, deleteMany passed")
}
func TestLegacyHello(t *testing.T) {
	addr, _ := start(t)
	c, e := net.Dial("tcp", addr)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	d, _ := bson.Marshal(bson.D{{Key: "isMaster", Value: 1}})
	p := make([]byte, 4)
	p = append(p, []byte("admin.$cmd\x00")...)
	p = append(p, 0, 0, 0, 0, 255, 255, 255, 255)
	p = append(p, d...)
	h := make([]byte, 16)
	binary.LittleEndian.PutUint32(h, uint32(16+len(p)))
	le.PutUint32(h[4:], 42)
	le.PutUint32(h[12:], 2004)
	if _, e = c.Write(append(h, p...)); e != nil {
		t.Fatal(e)
	}
	if _, e = io.ReadFull(c, h); e != nil {
		t.Fatal(e)
	}
	if le.Uint32(h[8:]) != 42 || le.Uint32(h[12:]) != 1 {
		t.Fatal("bad reply header")
	}
	p = make([]byte, int(le.Uint32(h))-16)
	if _, e = io.ReadFull(c, p); e != nil {
		t.Fatal(e)
	}
	var reply bson.M
	if e = bson.Unmarshal(p[20:], &reply); e != nil || reply["ismaster"] != true {
		t.Fatalf("%v %v", reply, e)
	}
}
func TestConfigAndMalformed(t *testing.T) {
	for _, raw := range []string{`{"users":{"u":"p"}}`, `{"maxDocuments":0}`, `{"unknown":1}`, `{} {}`} {
		if _, e := New([]byte(raw)); e == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, p := range [][]byte{nil, {0, 0, 0, 0, 1, 255, 255, 255, 255}, {0, 0, 0, 0, 0, 255, 255, 255, 255}} {
		if _, _, e := parse(2013, p); e == nil {
			t.Fatal("accepted malformed frame")
		}
	}
}

func TestShutdownClosesIdleConnection(t *testing.T) {
	s, _ := New(nil)
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	c, e := net.Dial("tcp", ln.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	cancel()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop")
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	if _, e = c.Read(make([]byte, 1)); e == nil {
		t.Fatal("connection remains open")
	}
}
func TestDocumentLimit(t *testing.T) {
	raw, e := New([]byte(`{"maxDocuments":1}`))
	if e != nil {
		t.Fatal(e)
	}
	s := raw.(*server)
	insert := bson.D{{Key: "insert", Value: "c"}, {Key: "$db", Value: "d"}, {Key: "documents", Value: bson.A{bson.D{{Key: "_id", Value: 1}}}}}
	if r := s.command(insert); r["ok"] != 1.0 {
		t.Fatal(r)
	}
	if r := s.command(insert); r["ok"] != 0.0 {
		t.Fatal("limit not enforced", r)
	}
}
