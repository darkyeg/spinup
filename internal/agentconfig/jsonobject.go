package agentconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type object struct {
	keys   []string
	values map[string]any
}

func newObject() *object { return &object{values: map[string]any{}} }

func (o *object) set(key string, value any) {
	if _, exists := o.values[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func decodeObject(data []byte) (*object, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	value, err := readValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the JSON value")
	}
	obj, ok := value.(*object)
	if !ok {
		return nil, errors.New("not a JSON object")
	}
	return obj, nil
}

func readValue(dec *json.Decoder) (any, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch delim, _ := token.(json.Delim); delim {
	case '{':
		return readMembers(dec)
	case '[':
		return readElements(dec)
	}
	return token, nil
}

func readMembers(dec *json.Decoder) (*object, error) {
	obj := newObject()
	for dec.More() {
		name, err := dec.Token()
		if err != nil {
			return nil, err
		}
		value, err := readValue(dec)
		if err != nil {
			return nil, err
		}
		obj.set(name.(string), value)
	}
	_, err := dec.Token()
	return obj, err
}

func readElements(dec *json.Decoder) ([]any, error) {
	elements := []any{}
	for dec.More() {
		value, err := readValue(dec)
		if err != nil {
			return nil, err
		}
		elements = append(elements, value)
	}
	_, err := dec.Token()
	return elements, err
}

func renderObject(o *object) string {
	var out strings.Builder
	writeValue(&out, o, 0)
	out.WriteString("\n")
	return out.String()
}

func writeValue(out *strings.Builder, value any, depth int) {
	switch v := value.(type) {
	case *object:
		writeContainer(out, "{", "}", depth, len(v.keys), func(i int) {
			out.WriteString(quote(v.keys[i]) + ": ")
			writeValue(out, v.values[v.keys[i]], depth+1)
		})
	case []any:
		writeContainer(out, "[", "]", depth, len(v), func(i int) { writeValue(out, v[i], depth+1) })
	case string:
		out.WriteString(quote(v))
	case json.Number:
		out.WriteString(v.String())
	case bool:
		fmt.Fprint(out, v)
	case nil:
		out.WriteString("null")
	}
}

func writeContainer(out *strings.Builder, open, close string, depth, count int, writeItem func(int)) {
	if count == 0 {
		out.WriteString(open + close)
		return
	}
	out.WriteString(open + "\n")
	for i := range count {
		out.WriteString(strings.Repeat("  ", depth+1))
		writeItem(i)
		if i < count-1 {
			out.WriteString(",")
		}
		out.WriteString("\n")
	}
	out.WriteString(strings.Repeat("  ", depth) + close)
}

func quote(text string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(text)
	return strings.TrimSuffix(buf.String(), "\n")
}
