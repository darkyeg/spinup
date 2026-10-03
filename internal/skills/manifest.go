package skills

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const (
	keyAgents  = "agents"
	keyManual  = "manual"
	keySources = "sources"
	keyComment = "_comment"
)

type sourceEntry struct {
	Source string
	Names  []string
}

type manifest struct {
	agents  []string
	manual  []string
	sources []sourceEntry
}

func (m manifest) listed() []string {
	var names []string
	for _, e := range m.sources {
		names = append(names, e.Names...)
	}
	return names
}

type document struct {
	obj     object
	agents  []string
	manual  []string
	sources []sourceEntry
}

func parseDocument(data []byte) (document, error) {
	obj, err := parseObject(data)
	if err != nil {
		return document{}, err
	}
	doc := document{obj: obj, manual: []string{}}
	if raw, ok := obj.get(keyAgents); ok {
		if err := json.Unmarshal(raw, &doc.agents); err != nil {
			return document{}, fmt.Errorf("agents: %w", err)
		}
	}
	if raw, ok := obj.get(keyManual); ok {
		if err := json.Unmarshal(raw, &doc.manual); err != nil {
			return document{}, fmt.Errorf("manual: %w", err)
		}
		if err := checkNames(doc.manual); err != nil {
			return document{}, fmt.Errorf("manual: %w", err)
		}
	}
	if raw, ok := obj.get(keySources); ok {
		if doc.sources, err = parseSources(raw); err != nil {
			return document{}, fmt.Errorf("sources: %w", err)
		}
	}
	for _, key := range []string{keySources, keyManual} {
		if _, ok := doc.obj.get(key); !ok {
			doc.obj.set(key, json.RawMessage("null"))
		}
	}
	return doc, nil
}

func parseSources(raw json.RawMessage) ([]sourceEntry, error) {
	obj, err := parseObject(raw)
	if err != nil {
		return nil, err
	}
	var entries []sourceEntry
	for _, source := range obj.keys {
		var names []string
		if err := json.Unmarshal(obj.values[source], &names); err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		if err := checkNames(names); err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		entries = append(entries, sourceEntry{source, names})
	}
	return entries, nil
}

func (d document) render() []byte {
	var out bytes.Buffer
	out.WriteString("{\n")
	for i, key := range d.obj.keys {
		if i > 0 {
			out.WriteString(",\n")
		}
		fmt.Fprintf(&out, "  %s: %s", encode(key), d.renderValue(key))
	}
	out.WriteString("\n}\n")
	return out.Bytes()
}

func (d document) renderValue(key string) string {
	switch key {
	case keySources:
		return renderSources(d.sources)
	case keyManual:
		return spaced(encode(nonNil(d.manual)))
	case keyAgents:
		return spaced(encode(nonNil(d.agents)))
	}
	return spaced(d.obj.values[key])
}

func renderSources(entries []sourceEntry) string {
	var rows []string
	for _, e := range entries {
		if len(e.Names) > 0 {
			rows = append(rows, fmt.Sprintf("    %s: %s", encode(e.Source), spaced(encode(e.Names))))
		}
	}
	if len(rows) == 0 {
		return "{}"
	}
	return "{\n" + strings.Join(rows, ",\n") + "\n  }"
}

func spaced(raw []byte) string {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		panic("invalid JSON: " + err.Error())
	}
	var out strings.Builder
	inString, escaped := false, false
	for _, c := range compact.Bytes() {
		out.WriteByte(c)
		switch {
		case escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case !inString && (c == ',' || c == ':'):
			out.WriteByte(' ')
		}
	}
	return out.String()
}

func (d *document) add(source string, names []string, mode Mode) {
	i := slices.IndexFunc(d.sources, func(e sourceEntry) bool { return e.Source == source })
	if i < 0 {
		d.sources = append(d.sources, sourceEntry{Source: source})
		i = len(d.sources) - 1
	}
	d.sources[i].Names = appendMissing(d.sources[i].Names, names)
	if mode == Manual {
		d.manual = sortedUnion(d.manual, names)
	}
}

func (d *document) remove(names []string) (found bool) {
	for i, e := range d.sources {
		found = found || slices.ContainsFunc(e.Names, func(n string) bool { return slices.Contains(names, n) })
		d.sources[i].Names = without(e.Names, names)
	}
	d.manual = without(d.manual, names)
	return found
}

func (d *document) setMode(names []string, mode Mode) {
	if mode == Manual {
		d.manual = sortedUnion(d.manual, names)
		return
	}
	d.manual = sortedUnion(without(d.manual, names), nil)
}

func (d document) manifest() manifest {
	return manifest{agents: d.agents, manual: d.manual, sources: d.sources}
}

func appendMissing(list, more []string) []string {
	for _, item := range more {
		if !slices.Contains(list, item) {
			list = append(list, item)
		}
	}
	return list
}

func sortedUnion(a, b []string) []string {
	union := append(slices.Clone(a), b...)
	slices.Sort(union)
	return slices.Compact(union)
}

func without(list, drop []string) []string {
	kept := []string{}
	for _, item := range list {
		if !slices.Contains(drop, item) {
			kept = append(kept, item)
		}
	}
	return kept
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
