package mock

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	lmcode "github.com/lmctl-ai/lmcode-go"
	"io"
	"net/url"
	"os"
	"strings"
	"time"
)

// Options holds trusted script configuration and optional HTTP fallback settings.
type Options struct {
	ConfigFile       string
	Upstream         string
	UpstreamTimeout  time.Duration
	UpstreamReadOnly bool
	AuditWriter      io.Writer
}

func helpers(reg *lmcode.Registry, options Options) {
	add := func(name string, params []string, ret string, fn lmcode.Function) {
		if err := reg.Function(name, params, ret, fn); err != nil {
			panic(err)
		}
	}
	str := func(name string, params []string, fn func([]lmcode.Value) (string, error)) {
		add(name, params, "string", func(a []lmcode.Value) (lmcode.Value, error) { s, e := fn(a); return lmcode.String(s), e })
	}
	str("config", nil, func([]lmcode.Value) (string, error) {
		if options.ConfigFile == "" {
			return "{}", nil
		}
		b, e := os.ReadFile(options.ConfigFile)
		if e != nil {
			return "", e
		}
		if !json.Valid(b) {
			return "", fmt.Errorf("invalid mock configuration JSON")
		}
		return string(b), nil
	})
	str("form_get", []string{"string", "string"}, func(a []lmcode.Value) (string, error) { v, e := url.ParseQuery(a[0].Str); return v.Get(a[1].Str), e })
	str("url_encode", []string{"string"}, func(a []lmcode.Value) (string, error) { return url.QueryEscape(a[0].Str), nil })
	str("url_decode", []string{"string"}, func(a []lmcode.Value) (string, error) { return url.QueryUnescape(a[0].Str) })
	str("json_quote", []string{"string"}, func(a []lmcode.Value) (string, error) { b, e := json.Marshal(a[0].Str); return string(b), e })
	for _, part := range []string{"id", "secret"} {
		str("basic_"+part, []string{"string"}, func(a []lmcode.Value) (string, error) {
			h := a[0].Str
			if !strings.HasPrefix(h, "Basic ") {
				return "", nil
			}
			b, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(h, "Basic "))
			if e != nil {
				return "", nil
			}
			v := strings.SplitN(string(b), ":", 2)
			if len(v) != 2 {
				return "", nil
			}
			i := 0
			if part == "secret" {
				i = 1
			}
			s, e := url.QueryUnescape(v[i])
			if e != nil {
				return "", nil
			}
			return s, nil
		})
	}
	str("random_token", nil, func([]lmcode.Value) (string, error) {
		b := make([]byte, 24)
		_, e := rand.Read(b)
		return base64.RawURLEncoding.EncodeToString(b), e
	})
	str("sha256_url", []string{"string"}, func(a []lmcode.Value) (string, error) {
		b := sha256.Sum256([]byte(a[0].Str))
		return base64.RawURLEncoding.EncodeToString(b[:]), nil
	})
	add("now_ms", nil, "int", func([]lmcode.Value) (lmcode.Value, error) { return lmcode.Int(time.Now().UnixMilli()), nil })
	add("secure_equal", []string{"string", "string"}, "bool", func(a []lmcode.Value) (lmcode.Value, error) {
		x, y := sha256.Sum256([]byte(a[0].Str)), sha256.Sum256([]byte(a[1].Str))
		return lmcode.Bool(subtle.ConstantTimeCompare(x[:], y[:]) == 1), nil
	})
}
