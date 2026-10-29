package mailstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPartialSpool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.jsonl")
	m := Message{From: "a@b", To: []string{"c@d"}, Data: []byte("Subject: x\r\n\r\nbody\r\n"), Date: Epoch}
	if err := Append(path, m); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(m)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(raw[:len(raw)/2])
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	messages, err := Read(path)
	if err != nil || len(messages) != 1 {
		t.Fatal("partial tail must be invisible", err)
	}
	f, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(append(raw[len(raw)/2:], '\n'))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	messages, err = Read(path)
	if err != nil || len(messages) != 2 {
		t.Fatal("completed tail missing", err)
	}
}
func TestMalformedSpoolAndUsers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.jsonl")
	if err := os.WriteFile(path, []byte("not-json\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("malformed spool accepted")
	}
	if !(Config{}).Accept("any", "any") {
		t.Fatal("default auth")
	}
	if (Config{Users: map[string]string{}}).Accept("any", "any") {
		t.Fatal("empty configured users accepted login")
	}
}

func TestConcurrentReadersSeeCompleteAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.jsonl")
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 25; i++ {
			if err := Append(path, Message{Data: []byte("Subject: test\r\n\r\nbody\r\n"), Date: Epoch}); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for {
		records, err := Read(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(records) > 25 {
			t.Fatal("duplicate records")
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			records, err = Read(path)
			if err != nil || len(records) != 25 {
				t.Fatal("missing records", err)
			}
			return
		default:
		}
	}
}
