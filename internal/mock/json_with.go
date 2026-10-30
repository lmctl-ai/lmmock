package mock

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	lmcode "github.com/lmctl-ai/lmcode-go"
)

// jsonWith creates an independent document. RawMessage preserves untouched
// number lexemes (including int64 values that cannot round-trip via float64).
// Paths are JSON Pointers. Parent containers must exist; "-" appends to arrays.
func jsonWith(document, pointer, replacement string) (string, error) {
	if !json.Valid([]byte(document)) || !json.Valid([]byte(replacement)) {
		return "", fmt.Errorf("json_with: invalid JSON")
	}
	if pointer == "" {
		return replacement, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return "", fmt.Errorf("json_with: expected JSON Pointer")
	}
	parts := strings.Split(pointer[1:], "/")
	for i, part := range parts {
		var b strings.Builder
		for j := 0; j < len(part); j++ {
			if part[j] != '~' {
				b.WriteByte(part[j])
				continue
			}
			j++
			if j == len(part) || (part[j] != '0' && part[j] != '1') {
				return "", fmt.Errorf("json_with: invalid pointer escape")
			}
			if part[j] == '0' {
				b.WriteByte('~')
			} else {
				b.WriteByte('/')
			}
		}
		parts[i] = b.String()
	}
	value, err := replaceJSON([]byte(document), parts, []byte(replacement))
	return string(value), err
}
func replaceJSON(doc json.RawMessage, path []string, value json.RawMessage) (json.RawMessage, error) {
	if len(path) == 0 {
		return value, nil
	}
	trimmed := strings.TrimSpace(string(doc))
	key := path[0]
	if strings.HasPrefix(trimmed, "{") {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(doc, &object); err != nil {
			return nil, err
		}
		child, exists := object[key]
		if !exists && len(path) > 1 {
			return nil, fmt.Errorf("json_with: parent does not exist")
		}
		updated, err := replaceJSON(child, path[1:], value)
		if err != nil {
			return nil, err
		}
		object[key] = updated
		return json.Marshal(object)
	}
	if strings.HasPrefix(trimmed, "[") {
		var array []json.RawMessage
		if err := json.Unmarshal(doc, &array); err != nil {
			return nil, err
		}
		if key == "-" && len(path) == 1 {
			return json.Marshal(append(array, value))
		}
		index, err := strconv.Atoi(key)
		if err != nil || strconv.Itoa(index) != key || index < 0 || index >= len(array) {
			return nil, fmt.Errorf("json_with: array index out of range")
		}
		array[index], err = replaceJSON(array[index], path[1:], value)
		if err != nil {
			return nil, err
		}
		return json.Marshal(array)
	}
	return nil, fmt.Errorf("json_with: parent is not an object or array")
}
func registerJSONWith(reg *lmcode.Registry) {
	err := reg.Function("json_with", []string{"Json", "string", "Json"}, "Json", func(a []lmcode.Value) (lmcode.Value, error) {
		doc, ok := a[0].Obj.(*lmcode.JSON)
		if !ok {
			return lmcode.None(), fmt.Errorf("json_with: expected Json document")
		}
		value, ok := a[2].Obj.(*lmcode.JSON)
		if !ok {
			return lmcode.None(), fmt.Errorf("json_with: expected Json value")
		}
		text, err := jsonWith(doc.ToJSON(), a[1].Str, value.ToJSON())
		if err != nil {
			return lmcode.None(), err
		}
		parsed, err := lmcode.ParseJSON(text)
		if err != nil {
			return lmcode.None(), err
		}
		return lmcode.Object("Json", parsed), nil
	})
	if err != nil {
		panic(err)
	}
}
