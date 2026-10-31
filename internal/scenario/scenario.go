// Package scenario converts lminspect recordings without retaining raw credentials.
// Replay policy belongs to the lmcode Handler in examples/scenario.lms.
package scenario

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

type Request struct {
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Scheme  string              `json:"scheme,omitempty"`
	Headers map[string][]string `json:"headers"`
	Content []byte              `json:"content"`
}
type Response struct {
	Status  int                 `json:"status_code"`
	Headers map[string][]string `json:"headers"`
	Content []byte              `json:"content"`
}
type Flow struct {
	Request   Request   `json:"request"`
	Response  *Response `json:"response"`
	Error     string    `json:"error,omitempty"`
	Timestamp int64     `json:"timestamp,omitempty"`
}
type Match struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	Body       string `json:"body_b64"`
	Auth       string `json:"auth"`
	AuthHeader string `json:"auth_header"`
}
type Reply struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    []byte            `json:"body_b64,omitempty"`
	Events  []string          `json:"events,omitempty"`
}
type Rule struct {
	ID    string `json:"id"`
	Group string `json:"group"`
	Nth   int    `json:"nth"`
	When  Match  `json:"when"`
	Reply Reply  `json:"reply"`
}
type Scenario struct {
	Format     string `json:"format"`
	Normalized bool   `json:"normalized"`
	Rules      []Rule `json:"rules"`
}

var patterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`), regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/=-]{12,}`), regexp.MustCompile(`AKIA[0-9A-Z]{16}`), regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`), regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
}
var ids = regexp.MustCompile(`\b(chatcmpl|msg|req|toolu|call|resp|cmpl)[_-][A-Za-z0-9]{8,}\b`)
var epochs = regexp.MustCompile(`("(?:created|created_at|timestamp|expires_at)"\s*:\s*)[0-9]{9,13}`)
var iso = regexp.MustCompile(`[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?(?:Z|[+-][0-9]{2}:[0-9]{2})`)

func header(h map[string][]string, key string) string {
	for k, v := range h {
		if strings.EqualFold(k, key) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}
func sensitive(k string) bool {
	k = strings.ToLower(k)
	return k == "authorization" || k == "proxy-authorization" || k == "cookie" || k == "set-cookie" || strings.Contains(k, "api-key") || strings.Contains(k, "token")
}

// Credential fields exclude protocol metadata such as tokenizer and token counts.
func credentialField(key string) bool {
	key = strings.ReplaceAll(strings.ToLower(key), "_", "-")
	return key == "key" || key == "apikey" || key == "api-key" || key == "token" || strings.HasSuffix(key, "-token") || strings.Contains(key, "password") || strings.Contains(key, "secret")
}

type normalizer struct {
	secrets []string
	names   map[string]string
	exact   bool
}

func (n *normalizer) text(s string) string {
	for _, secret := range n.secrets {
		s = strings.ReplaceAll(s, secret, "<redacted:credential>")
	}
	for _, p := range patterns {
		s = p.ReplaceAllString(s, "<redacted:secret>")
	}
	if n.exact {
		return s
	}
	s = ids.ReplaceAllStringFunc(s, func(id string) string {
		cut := strings.IndexAny(id, "_-")
		if strings.HasPrefix(id[cut+1:], "norm") {
			return id
		}
		if n.names[id] == "" {
			n.names[id] = fmt.Sprintf("%s-norm%04d", id[:cut], len(n.names)+1)
		}
		return n.names[id]
	})
	s = epochs.ReplaceAllString(s, "${1}1700000000")
	return iso.ReplaceAllString(s, "2000-01-01T00:00:00Z")
}
func (n *normalizer) body(b []byte, h map[string][]string) ([]byte, error) {
	encoding := strings.ToLower(header(h, "Content-Encoding"))
	if encoding == "gzip" {
		r, e := gzip.NewReader(bytes.NewReader(b))
		if e != nil {
			return nil, fmt.Errorf("invalid gzip body")
		}
		b, e = io.ReadAll(r)
		r.Close()
		if e != nil {
			return nil, fmt.Errorf("invalid gzip body")
		}
	} else if encoding != "" && encoding != "identity" {
		return nil, fmt.Errorf("unsupported content encoding")
	}
	if strings.Contains(header(h, "Content-Type"), "text/event-stream") {
		lines := strings.SplitAfter(string(b), "\n")
		for i, line := range lines {
			if strings.HasPrefix(line, "data:") {
				prefix := "data:"
				payload := strings.TrimPrefix(line, prefix)
				ending := ""
				if strings.HasSuffix(payload, "\r\n") {
					ending = "\r\n"
					payload = strings.TrimSuffix(payload, ending)
				} else if strings.HasSuffix(payload, "\n") {
					ending = "\n"
					payload = strings.TrimSuffix(payload, ending)
				}
				if strings.HasPrefix(payload, " ") {
					prefix += " "
					payload = payload[1:]
				}
				clean, e := n.body([]byte(payload), nil)
				if e != nil {
					return nil, e
				}
				lines[i] = prefix + string(clean) + ending
			}
		}
		b = []byte(strings.Join(lines, ""))
	}
	// Decode JSON string escapes before scrubbing. This prevents a credential encoded
	// as Unicode escapes from bypassing exact-value replacement. Re-encode only when
	// the decoded tree changes; ordinary byte formatting remains intact.
	var tree any
	if utf8.Valid(b) && decodeJSON(b, &tree) == nil {
		changed := false
		var walk func(any) any
		walk = func(v any) any {
			switch x := v.(type) {
			case string:
				y := n.text(x)
				changed = changed || y != x
				return y
			case []any:
				for i := range x {
					x[i] = walk(x[i])
				}
			case map[string]any:
				keys := make([]string, 0, len(x))
				for k := range x {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				out := map[string]any{}
				for _, k := range keys {
					nk := n.text(k)
					changed = changed || nk != k
					out[nk] = walk(x[k])
				}
				return out
			}
			return v
		}
		tree = walk(tree)
		if changed {
			b, _ = json.Marshal(tree)
		}
	}
	return []byte(n.text(string(b))), nil
}
func Events(text string) []string {
	var out []string
	for len(text) > 0 {
		end := -1
		size := 0
		for _, sep := range []string{"\r\n\r\n", "\n\n", "\r\r"} {
			if i := strings.Index(text, sep); i >= 0 && (end < 0 || i < end) {
				end = i
				size = len(sep)
			}
		}
		if end < 0 {
			out = append(out, text)
			break
		}
		out = append(out, text[:end+size])
		text = text[end+size:]
	}
	return out
}
func Import(input io.Reader, exact bool) (Scenario, []Flow, error) {
	var flows []Flow
	dec := json.NewDecoder(input)
	for {
		var f Flow
		e := dec.Decode(&f)
		if e == io.EOF {
			break
		}
		if e != nil {
			return Scenario{}, nil, fmt.Errorf("invalid capture at flow %d", len(flows)+1)
		}
		if f.Response == nil || f.Error != "" {
			return Scenario{}, nil, fmt.Errorf("flow %d has no complete HTTP response; transport failures cannot be replayed", len(flows)+1)
		}
		flows = append(flows, f)
	}
	if len(flows) == 0 {
		return Scenario{}, nil, fmt.Errorf("empty capture")
	}
	sort.SliceStable(flows, func(i, j int) bool { return flows[i].Timestamp < flows[j].Timestamp })
	n := normalizer{names: map[string]string{}, exact: exact}
	seen := map[string]bool{}
	add := func(s string) {
		if s == "<redacted:credential>" || s == "<redacted:secret>" {
			return
		}
		if s != "" && !regexp.MustCompile(`^(Bearer )?fixture-credential(?:-[0-9]+)?$`).MatchString(s) && !seen[s] {
			seen[s] = true
			n.secrets = append(n.secrets, s)
		}
	}
	// Discover named credentials before scrubbing, including echoes in earlier flows.
	for _, f := range flows {
		if u, err := url.ParseRequestURI(f.Request.Path); err == nil {
			for key, values := range u.Query() {
				if credentialField(key) {
					for _, value := range values {
						add(value)
						add(url.QueryEscape(value))
					}
				}
			}
		}
		for _, part := range []struct {
			body    []byte
			headers map[string][]string
		}{{f.Request.Content, f.Request.Headers}, {f.Response.Content, f.Response.Headers}} {
			plain := normalizer{exact: true}
			body, err := plain.body(part.body, part.headers)
			if err != nil {
				return Scenario{}, nil, err
			}
			var tree any
			if decodeJSON(body, &tree) == nil {
				var discover func(any, bool)
				discover = func(v any, named bool) {
					switch x := v.(type) {
					case string:
						if named {
							add(x)
						}
					case []any:
						for _, value := range x {
							discover(value, named)
						}
					case map[string]any:
						for key, value := range x {
							discover(value, named || credentialField(key))
						}
					}
				}
				discover(tree, false)
			}
		}
	}
	// Gather first, scrub second: a secret in an earlier response may first appear
	// in a later request header. Values are never included in diagnostics.
	for _, f := range flows {
		for _, h := range []map[string][]string{f.Request.Headers, f.Response.Headers} {
			for k, vs := range h {
				if sensitive(k) {
					for _, v := range vs {
						add(v)
						fields := strings.Fields(v)
						if len(fields) == 2 && (strings.EqualFold(fields[0], "Bearer") || strings.EqualFold(fields[0], "Basic")) {
							add(fields[1])
						}
						if strings.Contains(strings.ToLower(k), "cookie") {
							pairs := strings.Split(v, ";")
							if strings.EqualFold(k, "Set-Cookie") {
								pairs = pairs[:1]
							}
							for _, pair := range pairs {
								if _, v, ok := strings.Cut(pair, "="); ok {
									add(strings.TrimSpace(v))
								}
							}
						}
					}
				}
			}
		}
	}
	sort.Slice(n.secrets, func(i, j int) bool {
		if len(n.secrets[i]) == len(n.secrets[j]) {
			return n.secrets[i] < n.secrets[j]
		}
		return len(n.secrets[i]) > len(n.secrets[j])
	})
	s := Scenario{Format: "lmmock-scenario/1", Normalized: !exact}
	groups := map[string]string{}
	counts := map[string]int{}
	authIDs := map[string]string{}
	for i := range flows {
		f := &flows[i]
		rq := &f.Request
		rp := f.Response
		if rq.Method == "" || !strings.HasPrefix(rq.Path, "/") || strings.HasPrefix(rq.Path, "//") || strings.ContainsAny(rq.Path, "\r\n") || rp.Status < 200 || rp.Status > 599 {
			return Scenario{}, nil, fmt.Errorf("invalid method/path/status at flow %d", i+1)
		}
		authName := ""
		auth := ""
		for _, key := range []string{"Authorization", "X-Api-Key", "Cookie"} {
			if header(rq.Headers, key) != "" {
				authName = key
				value := header(rq.Headers, key)
				if strings.HasPrefix(value, "Bearer ") {
					value = strings.TrimPrefix(value, "Bearer ")
				}
				if regexp.MustCompile(`^fixture-credential(?:-[0-9]+)?$`).MatchString(value) {
					auth = value
				} else {
					if authIDs[value] == "" {
						authIDs[value] = fmt.Sprintf("fixture-credential-%d", len(authIDs)+1)
					}
					auth = authIDs[value]
				}
				break
			}
		}
		body, e := n.body(rq.Content, rq.Headers)
		if e != nil {
			return Scenario{}, nil, e
		}
		rq.Content = body
		if uri, err := url.ParseRequestURI(rq.Path); err == nil {
			query := uri.Query()
			changed := false
			for key, values := range query {
				if credentialField(key) {
					for index := range values {
						values[index] = "<redacted:credential>"
					}
					query[key] = values
					changed = true
				}
			}
			if changed {
				uri.RawQuery = query.Encode()
				rq.Path = uri.String()
			}
		}
		rq.Path = n.text(rq.Path)
		rq.Scheme = "http"
		requestHeaders := map[string][]string{}
		if ct := header(rq.Headers, "content-type"); ct != "" {
			requestHeaders["Content-Type"] = []string{n.text(ct)}
		}
		if authName != "" {
			v := auth
			if authName == "Authorization" {
				v = "Bearer " + v
			}
			requestHeaders[authName] = []string{v}
		}
		rq.Headers = requestHeaders
		body, e = n.body(rp.Content, rp.Headers)
		if e != nil {
			return Scenario{}, nil, e
		}
		rp.Content = body
		hs := map[string]string{}
		for k, vs := range rp.Headers {
			k = strings.ToLower(k)
			if k == "content-type" || k == "cache-control" || k == "retry-after" || k == "www-authenticate" || strings.HasPrefix(k, "x-ratelimit-") {
				if len(vs) != 1 {
					return Scenario{}, nil, fmt.Errorf("multiple replay header values at flow %d", i+1)
				}
				hs[k] = n.text(vs[0])
			}
		}
		rp.Headers = map[string][]string{}
		for k, v := range hs {
			rp.Headers[k] = []string{v}
		}
		encoded, _ := json.Marshal(rq.Content)
		var body64 string
		_ = json.Unmarshal(encoded, &body64)
		m := Match{rq.Method, rq.Path, body64, auth, authName}
		keyBytes, _ := json.Marshal(m)
		key := string(keyBytes)
		if groups[key] == "" {
			groups[key] = fmt.Sprintf("g%d", len(groups)+1)
		}
		counts[key]++
		reply := Reply{Status: rp.Status, Headers: hs, Body: rp.Content}
		if strings.Contains(hs["content-type"], "text/event-stream") {
			if !utf8.Valid(body) {
				return Scenario{}, nil, fmt.Errorf("non-UTF8 SSE")
			}
			reply.Events = Events(string(body))
			reply.Body = nil
		}
		s.Rules = append(s.Rules, Rule{fmt.Sprintf("r%d", i+1), groups[key], counts[key], m, reply})
		f.Timestamp = int64(i)
	}
	// Scan decoded fields as well as the exported JSON. Base64 must not hide leaks.
	var outputs []string
	a, _ := json.Marshal(s)
	outputs = append(outputs, string(a))
	for _, f := range flows {
		outputs = append(outputs, f.Request.Path, string(f.Request.Content), string(f.Response.Content))
	}
	for _, out := range outputs {
		for _, secret := range n.secrets {
			if strings.Contains(out, secret) {
				return Scenario{}, nil, fmt.Errorf("unscrubbed credential residue")
			}
		}
		for _, p := range patterns {
			if p.MatchString(out) {
				return Scenario{}, nil, fmt.Errorf("unscrubbed secret residue")
			}
		}
	}
	return s, flows, nil
}

// Verify never follows redirects, uses environment proxies or contacts a vendor.
func Verify(input io.Reader, target string) error {
	s, flows, e := Import(input, false)
	if e != nil {
		return e
	}
	u, e := url.Parse(target)
	if e != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Path != "" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
		return fmt.Errorf("verify target must be a loopback http origin")
	}
	client := http.Client{Transport: &http.Transport{Proxy: nil, DisableCompression: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	var replay bytes.Buffer
	for i, f := range flows {
		request, e := http.NewRequest(f.Request.Method, target+f.Request.Path, bytes.NewReader(f.Request.Content))
		if e != nil {
			return fmt.Errorf("invalid replay request %d", i+1)
		}
		request.Header = http.Header(f.Request.Headers)
		response, e := client.Do(request)
		if e != nil {
			return fmt.Errorf("replay transport failed at flow %d", i+1)
		}
		b, e := io.ReadAll(response.Body)
		response.Body.Close()
		if e != nil {
			return fmt.Errorf("replay body failed at flow %d", i+1)
		}
		f.Response = &Response{response.StatusCode, map[string][]string(response.Header), b}
		json.NewEncoder(&replay).Encode(f)
	}
	got, _, e := Import(&replay, false)
	if e != nil {
		return e
	}
	for i := range s.Rules {
		a, _ := json.Marshal(s.Rules[i].Reply)
		b, _ := json.Marshal(got.Rules[i].Reply)
		if !bytes.Equal(a, b) {
			return fmt.Errorf("FIDELITY DIFF at flow %d", i+1)
		}
	}
	return nil
}

func decodeJSON(b []byte, v *any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
