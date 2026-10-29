package kafka

import (
	"context"
	"net"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func start(t *testing.T, cfg string) (string, func()) {
	t.Helper()
	s, err := New([]byte(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if s.Scheme() != "kafka" {
		t.Fatal(s.Scheme())
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	stop := func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("shutdown timed out")
		}
	}
	return ln.Addr().String(), stop
}
func TestRoundTrip(t *testing.T) {
	addr, stop := start(t, `{"topics":{"seeded":2}}`)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	producer, err := kgo.NewClient(kgo.SeedBrokers(addr), kgo.AllowAutoTopicCreation(), kgo.RecordPartitioner(kgo.ManualPartitioner()))
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	req := kmsg.NewPtrMetadataRequest()
	res, err := req.RequestWith(ctx, producer)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Brokers) != 1 || net.JoinHostPort(res.Brokers[0].Host, strconv.Itoa(int(res.Brokers[0].Port))) != addr {
		t.Fatalf("advertised brokers: %+v", res.Brokers)
	}
	found := false
	for _, topic := range res.Topics {
		if topic.Topic != nil && *topic.Topic == "seeded" {
			found = len(topic.Partitions) == 2
		}
	}
	if !found {
		t.Fatal("seeded partitions missing")
	}
	want := []string{"first", "héllo \"Kafka\"\nsecond", "third", ""}
	for i, v := range want {
		r := &kgo.Record{Topic: "roundtrip", Partition: 0, Value: []byte(v)}
		if err := producer.ProduceSync(ctx, r).FirstErr(); err != nil {
			t.Fatal(err)
		}
		if r.Offset != int64(i) {
			t.Fatalf("offset %d", r.Offset)
		}
	}
	// New clients explicitly assign and seek earliest, so both reads replay.
	for attempt := 0; attempt < 2; attempt++ {
		consumer, err := kgo.NewClient(kgo.SeedBrokers(addr), kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{"roundtrip": {0: kgo.NewOffset().AtStart()}}))
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for len(got) < len(want) {
			fetched := consumer.PollFetches(ctx)
			if errs := fetched.Errors(); len(errs) > 0 {
				consumer.Close()
				t.Fatal(errs)
			}
			fetched.EachRecord(func(r *kgo.Record) { got = append(got, string(r.Value)) })
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %q", got)
		}
		short, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
		fetched := consumer.PollFetches(short)
		cancel()
		consumer.Close()
		if fetched.NumRecords() != 0 {
			t.Fatal("expected no records past end")
		}
	}
}
func TestShutdown(t *testing.T) {
	addr, stop := start(t, "")
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Partial Kafka length prefix must not hold shutdown open.
	conn.Write([]byte{0, 0})
	stop()
	conn.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	if _, err := conn.Read(b[:]); err == nil {
		t.Fatal("socket still open")
	}
	if c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("listener still open")
	}
}
func TestConfig(t *testing.T) {
	for _, cfg := range []string{`null`, `[]`, `{"brokers":2}`, `{"topics":{"bad name":1}}`, `{"topics":{"..":1}}`, `{"topics":{"t":0}}`, `{"topics":{"t":101}}`, `{"topics":{"t":1.5}}`, `{} {}`, `{`} {
		if _, err := New([]byte(cfg)); err == nil || !strings.HasPrefix(err.Error(), "config:") {
			t.Errorf("config %s: %v", cfg, err)
		}
	}
}
