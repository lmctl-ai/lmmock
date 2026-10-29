// Package check runs declarative HTTP assertion suites without printing response secrets.
package check

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Step struct {
	Name         string            `json:"name"`
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	Headers      map[string]string `json:"headers"`
	Body         any               `json:"body"`
	Status       int               `json:"status"`
	JSON         map[string]any    `json:"json"`
	Contains     string            `json:"contains"`
	Capture      map[string]string `json:"capture"`
	TimeoutMS    int               `json:"timeoutMs"`
	NetworkError bool              `json:"networkError"`
}
type Suite struct {
	Steps []Step `json:"steps"`
}

func lookup(v any, path string) (any, bool) {
	for _, part := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			var ok bool
			v, ok = x[part]
			if !ok {
				return nil, false
			}
		case []any:
			i, e := strconv.Atoi(part)
			if e != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}
func expand(v any, vars map[string]string) any {
	switch x := v.(type) {
	case string:
		x = regexp.MustCompile(`\$\{([^}]+)\}`).ReplaceAllStringFunc(x, func(key string) string {
			if value, ok := vars[key[2:len(key)-1]]; ok {
				return value
			}
			return key
		})
		return x
	case map[string]any:
		r := map[string]any{}
		for k, value := range x {
			r[k] = expand(value, vars)
		}
		return r
	case []any:
		r := make([]any, len(x))
		for i, value := range x {
			r[i] = expand(value, vars)
		}
		return r
	}
	return v
}
func Run(ctx context.Context, base string, s Suite) (int, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return 0, fmt.Errorf("a valid HTTP(S) base URL is required")
	}
	if len(s.Steps) == 0 {
		return 0, fmt.Errorf("suite must contain at least one step")
	}
	vars := map[string]string{}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for i, step := range s.Steps {
		fail := func(reason string) (int, error) { return i, fmt.Errorf("step %d (%s): %s", i+1, step.Name, reason) }
		method := step.Method
		if method == "" {
			method = "GET"
		}
		target := strings.TrimRight(base, "/") + "/" + strings.TrimLeft(expand(step.Path, vars).(string), "/")
		var body []byte
		if step.Body != nil {
			v := expand(step.Body, vars)
			if text, ok := v.(string); ok {
				body = []byte(text)
			} else {
				body, err = json.Marshal(v)
				if err != nil {
					return fail("invalid request body")
				}
			}
		}
		timeout := step.TimeoutMS
		if timeout == 0 {
			timeout = 5000
		}
		if timeout < 1 || timeout > 60000 {
			return fail("timeoutMs must be 1..60000")
		}
		call, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
		request, err := http.NewRequestWithContext(call, method, target, bytes.NewReader(body))
		if err != nil {
			cancel()
			return fail("invalid request")
		}
		if step.Body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		for k, v := range step.Headers {
			request.Header.Set(k, expand(v, vars).(string))
		}
		response, err := client.Do(request)
		if err != nil {
			cancel()
			if step.NetworkError {
				continue
			}
			return fail("HTTP request failed")
		}
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
		response.Body.Close()
		cancel()
		if step.NetworkError {
			if readErr != nil {
				continue
			}
			return fail("expected a network error")
		}
		if readErr != nil || len(payload) > 4<<20 {
			return fail("response read failed or exceeded 4 MiB")
		}
		status := step.Status
		if status == 0 {
			status = 200
		}
		if response.StatusCode != status {
			return fail(fmt.Sprintf("expected status %d, got %d", status, response.StatusCode))
		}
		if step.Contains != "" && !strings.Contains(string(payload), expand(step.Contains, vars).(string)) {
			return fail("response did not contain expected text")
		}
		if len(step.JSON) > 0 || len(step.Capture) > 0 {
			var value any
			if json.Unmarshal(payload, &value) != nil {
				return fail("expected valid JSON")
			}
			for path, want := range step.JSON {
				got, ok := lookup(value, path)
				if !ok || !reflect.DeepEqual(got, expand(want, vars)) {
					return fail("JSON assertion failed at " + path)
				}
			}
			for name, path := range step.Capture {
				v, ok := lookup(value, path)
				if !ok {
					return fail("capture path missing: " + path)
				}
				text, ok := v.(string)
				if !ok {
					b, _ := json.Marshal(v)
					text = string(b)
				}
				vars[name] = text
			}
		}
	}
	return len(s.Steps), nil
}
