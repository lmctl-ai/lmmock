package scenario

import (
	"strings"
	"testing"
)

func TestRequestLogEmptyAndQuotedCredentials(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		empty := RedactRequest(Request{Path: "/", Headers: map[string][]string{"Authorization": {""}}, Content: []byte(`{"message":"ok"}`)})
		if string(empty.Content) != `{"message":"ok"}` {
			t.Fatal("empty credential changed ordinary log body")
		}
	})
	cases := map[string]Request{
		"quoted-cookie": {Path: "/", Headers: map[string][]string{"Cookie": {`session="synthetic-cookie-secret"`}}, Content: []byte(`{"echo":"synthetic-cookie-secret"}`)},
		"query-key":     {Path: "/generate?key=synthetic-query-secret", Headers: map[string][]string{}, Content: []byte(`{"echo":"synthetic-query-secret"}`)},
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			got := RedactRequest(request)
			if strings.Contains(got.Path+string(got.Content), "synthetic-") {
				t.Fatal("quoted cookie/query credential escaped request-log scrubbing")
			}
		})
	}
}
