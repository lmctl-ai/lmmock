package jsontemplate

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func decoded(t *testing.T, s string) any {
	t.Helper()
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
func TestRender(t *testing.T) {
	cases := []struct{ name, template, context, want string }{
		{"nested", "{\"greeting\":\"Hello, <user.name>!\",\"profile\":\"<user.profile>\",\"first\":\"<items.0.id>\"}", "{\"user\":{\"name\":\"World\",\"profile\":{\"enabled\":true}},\"items\":[{\"id\":7}]}", "{\"greeting\":\"Hello, World!\",\"profile\":{\"enabled\":true},\"first\":7}"},
		{"types", "[\"<s>\",\"<n>\",\"<b>\",\"<nil>\",\"<array>\",\"<object>\"]", "{\"s\":\"quoted \\\"\\n🦊\",\"n\":9223372036854775807,\"b\":false,\"nil\":null,\"array\":[1,2],\"object\":{\"a\":1}}", "[\"quoted \\\"\\n🦊\",9223372036854775807,false,null,[1,2],{\"a\":1}]"},
		{"inline", "\"<s>: <n>, <b>, <nil>\"", "{\"s\":\"hi\",\"n\":1.20e3,\"b\":true,\"nil\":null}", "\"hi: 1.20e3, true, null\""},
		{"root", "\"<data>\"", "{\"data\":{\"a\":[true,null]}}", "{\"a\":[true,null]}"},
		{"single pass", "[\"<data>\",\"prefix <text>\"]", "{\"data\":{\"x\":\"<missing>\"},\"text\":\"<missing>\"}", "[{\"x\":\"<missing>\"},\"prefix <missing>\"]"},
		{"keys", "{\"<missing>\":\"ok\"}", "{}", "{\"<missing>\":\"ok\"}"},
		{"literal", "\"<<missing> <x> << <<x> ${lmscript} {{other}}\"", "{\"x\":\"yes\"}", "\"<missing> yes < <x> ${lmscript} {{other}}\""},
		{"static", "[1,null,true,\"2 < 3\",{}]", "{}", "[1,null,true,\"2 < 3\",{}]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Render(tc.template, tc.context)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded(t, got), decoded(t, tc.want)) {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
			again, err := Render(tc.template, tc.context)
			if err != nil || again != got {
				t.Fatalf("render is not repeatable: %s %v", again, err)
			}
		})
	}
}
func TestRenderErrors(t *testing.T) {
	cases := []struct{ name, template, context string }{
		{"missing", "\"<missing>\"", "{}"},
		{"nested missing", "\"<user.name>\"", "{\"user\":{}}"},
		{"scalar parent", "\"<user.name>\"", "{\"user\":true}"},
		{"array bounds", "\"<items.2>\"", "{\"items\":[0]}"},
		{"array canonical", "\"<items.00>\"", "{\"items\":[0]}"},
		{"inline object", "\"hi <user>\"", "{\"user\":{}}"},
		{"inline array", "\"hi <items>\"", "{\"items\":[]}"},
		{"bad template", "{", "{}"},
		{"bad scalar", "true false", "{}"},
		{"bad context", "{}", "{"},
		{"null context", "{}", "null"},
		{"array context", "{}", "[]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Render(tc.template, tc.context); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
func FuzzRender(f *testing.F) {
	f.Add("\"<value>\"", "{\"value\":1}")
	f.Add("\"<<value>\"", "{}")
	f.Fuzz(func(t *testing.T, template, context string) {
		got, err := Render(template, context)
		if err == nil && !json.Valid([]byte(got)) {
			t.Fatalf("invalid output: %q", got)
		}
	})
}
func TestNumberLexemes(t *testing.T) {
	got, err := Render("[\"<n>\",\"<exponent>\"]", "{\"n\":9223372036854775807,\"exponent\":1.20e3}")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal([]byte(got), []byte("[9223372036854775807,1.20e3]")) {
		t.Fatal(got)
	}
}
