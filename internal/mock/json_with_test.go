package mock

import (
	"net/http/httptest"
	"testing"
)

func TestJSONWith(t *testing.T) {
	cases := []struct{ doc, path, value, want string }{
		{"{\"n\":9223372036854775807,\"nested\":{\"text\":\"\"}}", "/nested/text", "\"quote \\\" and \\n newline\"", "{\"n\":9223372036854775807,\"nested\":{\"text\":\"quote \\\" and \\n newline\"}}"},
		{"{\"a/b\":{\"~key\":[1,true,null]}}", "/a~1b/~0key/1", "{\"v\":\"literal\"}", "{\"a/b\":{\"~key\":[1,{\"v\":\"literal\"},null]}}"},
		{"{\"items\":[]}", "/items/-", "{\"id\":1}", "{\"items\":[{\"id\":1}]}"},
		{"{\"items\":[1]}", "/items/0", "null", "{\"items\":[null]}"},
		{"{}", "/new", "false", "{\"new\":false}"},
		{"{}", "", "[1,2]", "[1,2]"},
	}
	for _, tc := range cases {
		got, err := jsonWith(tc.doc, tc.path, tc.value)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("got %s, want %s", got, tc.want)
		}
	}
	for _, path := range []string{"bad", "/missing/child", "/items/2", "/items/-/child", "/items/01", "/items/-1", "/bad~2"} {
		if _, err := jsonWith("{\"items\":[1]}", path, "0"); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	if _, err := jsonWith("{}", "/x", "broken"); err == nil {
		t.Fatal("accepted invalid value")
	}
}
func TestPreviousSchemaStillRuns(t *testing.T) {
	engine, err := New(fixture(t, "legacy"))
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 || recorder.Body.String() != "ok" {
		t.Fatalf("legacy handler: %d %s", recorder.Code, recorder.Body.String())
	}
}
