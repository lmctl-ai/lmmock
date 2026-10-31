package scenario

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
)

func logSensitive(key string) bool {
	key = strings.ReplaceAll(strings.ToLower(key), "_", "-")
	return sensitive(key) || strings.Contains(key, "password") || strings.Contains(key, "secret")
}

// RedactRequest shares the converter's credential/pattern scrubber without
// normalizing IDs or timestamps. Encoded/binary bodies are omitted from logs.
func RedactRequest(request Request) Request {
	n := normalizer{exact: true}
	for key, values := range request.Headers {
		if logSensitive(key) {
			for _, value := range values {
				n.secrets = append(n.secrets, value, strings.TrimPrefix(value, "Bearer "))
				if strings.EqualFold(key, "Cookie") {
					for _, part := range strings.Split(value, ";") {
						_, secret, ok := strings.Cut(strings.TrimSpace(part), "=")
						if ok && secret != "" {
							n.secrets = append(n.secrets, secret)
						}
					}
				}
				if strings.HasPrefix(value, "Basic ") {
					if raw, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "Basic ")); e == nil {
						for _, part := range strings.SplitN(string(raw), ":", 2) {
							if part != "" {
								n.secrets = append(n.secrets, part)
							}
						}
					}
				}
			}
		}
	}
	uri, err := url.ParseRequestURI(request.Path)
	if err == nil {
		query := uri.Query()
		for key, values := range query {
			if logSensitive(key) {
				n.secrets = append(n.secrets, values...)
				query.Set(key, "[redacted]")
			}
		}
		uri.RawQuery = query.Encode()
	}
	sort.Slice(n.secrets, func(i, j int) bool { return len(n.secrets[i]) > len(n.secrets[j]) })
	clean := Request{Method: request.Method, Scheme: request.Scheme, Headers: map[string][]string{}}
	clean.Path = n.text(request.Path)
	if err == nil {
		uri.Path = n.text(uri.Path)
		uri.RawPath = ""
		query := uri.Query()
		for key, values := range query {
			for i, value := range values {
				values[i] = n.text(value)
			}
			query[key] = values
		}
		uri.RawQuery = query.Encode()
		clean.Path = uri.String()
	}
	for key, values := range request.Headers {
		if logSensitive(key) {
			clean.Headers[key] = []string{"[redacted]"}
		} else {
			for _, value := range values {
				clean.Headers[key] = append(clean.Headers[key], n.text(value))
			}
		}
	}
	if !utf8.Valid(request.Content) || (header(request.Headers, "Content-Encoding") != "" && header(request.Headers, "Content-Encoding") != "identity") {
		clean.Content = []byte("[encoded body omitted]")
		return clean
	}
	body := request.Content
	var tree any
	if decodeJSON(body, &tree) == nil {
		var walk func(any) any
		walk = func(value any) any {
			switch x := value.(type) {
			case map[string]any:
				for key, v := range x {
					if logSensitive(key) {
						x[key] = "[redacted]"
					} else {
						x[key] = walk(v)
					}
				}
			case []any:
				for i, v := range x {
					x[i] = walk(v)
				}
			}
			return value
		}
		body, _ = json.Marshal(walk(tree))
	}
	clean.Content, _ = n.body(body, nil)
	return clean
}
