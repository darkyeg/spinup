package skills

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type object struct {
	keys   []string
	values map[string]json.RawMessage
}

func newObject() object { return object{values: map[string]json.RawMessage{}} }

func parseObject(data []byte) (object, error) {
	obj := newObject()
	if len(bytes.TrimSpace(data)) == 0 {
		return obj, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	opening, err := dec.Token()
	if err != nil {
		return obj, err
	}
	if opening == nil {
		return obj, nil
	}
	if opening != json.Delim('{') {
		return obj, fmt.Errorf("expected a JSON object")
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return obj, err
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return obj, err
		}
		obj.set(key.(string), value)
	}
	return obj, nil
}

func (o object) get(key string) (json.RawMessage, bool) {
	value, ok := o.values[key]
	return value, ok
}

func (o *object) set(key string, value json.RawMessage) {
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o *object) remove(key string) {
	if _, ok := o.values[key]; !ok {
		return
	}
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

func (o object) indented() []byte {
	var out bytes.Buffer
	if err := json.Indent(&out, o.compact(), "", "  "); err != nil {
		panic("object holds invalid JSON: " + err.Error())
	}
	out.WriteByte('\n')
	return out.Bytes()
}

func (o object) compact() json.RawMessage {
	var joined, out bytes.Buffer
	joined.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			joined.WriteByte(',')
		}
		joined.Write(encode(key))
		joined.WriteByte(':')
		joined.Write(o.values[key])
	}
	joined.WriteByte('}')
	if err := json.Compact(&out, joined.Bytes()); err != nil {
		panic("object holds invalid JSON: " + err.Error())
	}
	return out.Bytes()
}

func encode(v any) json.RawMessage {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic("cannot encode JSON: " + err.Error())
	}
	return bytes.TrimSpace(out.Bytes())
}
