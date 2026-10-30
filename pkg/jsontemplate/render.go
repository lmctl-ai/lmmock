// Package jsontemplate renders JSON templates using typed context values.
package jsontemplate

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var placeholder = regexp.MustCompile("<<|<([A-Za-z_][A-Za-z0-9_]*(?:[.](?:[A-Za-z_][A-Za-z0-9_]*|[0-9]+))*)>")

// Render replaces <path> placeholders in JSON string values using a JSON object
// context. Whole-value placeholders retain their JSON type; embedded placeholders
// accept scalars only. Dotted paths traverse objects and zero-based array indexes.
// Use << for a literal <. Keys and inserted values are never interpreted.
// Inputs are immutable. Missing paths and invalid JSON return an error.
func Render(template, context string) (string, error) {
	var bindings map[string]json.RawMessage
	if !strings.HasPrefix(strings.TrimSpace(context), "{") {
		return "", fmt.Errorf("json_render: context must be an object")
	}
	if err := json.Unmarshal([]byte(context), &bindings); err != nil {
		return "", err
	}
	result, err := renderJSON(json.RawMessage(template), bindings)
	return string(result), err
}
func renderJSON(value json.RawMessage, bindings map[string]json.RawMessage) (json.RawMessage, error) {
	text := strings.TrimSpace(string(value))
	if text == "" {
		return nil, fmt.Errorf("json_render: invalid template")
	}
	switch text[0] {
	case '{':
		var object map[string]json.RawMessage
		if err := json.Unmarshal(value, &object); err != nil {
			return nil, err
		}
		for k, v := range object {
			updated, err := renderJSON(v, bindings)
			if err != nil {
				return nil, err
			}
			object[k] = updated
		}
		return json.Marshal(object)
	case '[':
		var array []json.RawMessage
		if err := json.Unmarshal(value, &array); err != nil {
			return nil, err
		}
		for i, v := range array {
			updated, err := renderJSON(v, bindings)
			if err != nil {
				return nil, err
			}
			array[i] = updated
		}
		return json.Marshal(array)
	case '"':
		var source string
		if err := json.Unmarshal(value, &source); err != nil {
			return nil, err
		}
		matches := placeholder.FindAllStringSubmatchIndex(source, -1)
		if len(matches) == 0 {
			return value, nil
		}
		if len(matches) == 1 && matches[0][2] >= 0 && matches[0][0] == 0 && matches[0][1] == len(source) {
			name := source[matches[0][2]:matches[0][3]]
			replacement, ok := lookupJSONContext(bindings, name)
			if !ok {
				return nil, fmt.Errorf("json_render: missing variable %q", name)
			}
			return replacement, nil
		}
		var out strings.Builder
		previous := 0
		for _, m := range matches {
			out.WriteString(source[previous:m[0]])
			if m[2] < 0 {
				out.WriteByte('<')
				previous = m[1]
				continue
			}
			name := source[m[2]:m[3]]
			replacement, ok := lookupJSONContext(bindings, name)
			if !ok {
				return nil, fmt.Errorf("json_render: missing variable %q", name)
			}
			var scalar string
			raw := strings.TrimSpace(string(replacement))
			if raw[0] == '"' {
				if err := json.Unmarshal(replacement, &scalar); err != nil {
					return nil, err
				}
			} else {
				if strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[") {
					return nil, fmt.Errorf("json_render: variable %q needs a whole-value placeholder", name)
				}
				scalar = raw
			}
			out.WriteString(scalar)
			previous = m[1]
		}
		out.WriteString(source[previous:])
		return json.Marshal(out.String())
	default:
		if !json.Valid(value) {
			return nil, fmt.Errorf("json_render: invalid JSON")
		}
		return value, nil
	}
}

// Context paths traverse objects and arrays; returned subtrees remain JSON.
func lookupJSONContext(bindings map[string]json.RawMessage, path string) (json.RawMessage, bool) {
	parts := strings.Split(path, ".")
	value, ok := bindings[parts[0]]
	if !ok {
		return nil, false
	}
	for _, key := range parts[1:] {
		text := strings.TrimSpace(string(value))
		if strings.HasPrefix(text, "{") {
			var object map[string]json.RawMessage
			if json.Unmarshal(value, &object) != nil {
				return nil, false
			}
			value, ok = object[key]
			if !ok {
				return nil, false
			}
		} else if strings.HasPrefix(text, "[") {
			var array []json.RawMessage
			if json.Unmarshal(value, &array) != nil {
				return nil, false
			}
			i, err := strconv.Atoi(key)
			if err != nil || strconv.Itoa(i) != key || i < 0 || i >= len(array) {
				return nil, false
			}
			value = array[i]
		} else {
			return nil, false
		}
	}
	return value, true
}
