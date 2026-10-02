package agentconfig

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var tableHeader = regexp.MustCompile(`^\s*\[`)

type lineSpan struct{ start, end int }

func literal(value any) (string, error) {
	switch v := value.(type) {
	case bool:
		return strconv.FormatBool(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return floatLiteral(v), nil
	case string:
		return quote(v), nil
	}
	return "", fmt.Errorf("unsupported TOML value: %v", value)
}

func floatLiteral(v float64) string {
	text := strconv.FormatFloat(v, 'g', -1, 64)
	if strings.ContainsAny(text, ".eE") {
		return text
	}
	return text + ".0"
}

func setKey(text, table, key, value string) string {
	lines := splitLines(text)
	span, found := locateTable(lines, table)
	if !found {
		return strings.TrimRight(text, "\n") + "\n\n[" + table + "]\n" + key + " = " + value + "\n"
	}
	line := key + " = " + value
	if at := keyLine(lines, span, key); at >= 0 {
		lines[at] = line
		return joinLines(lines)
	}
	insert := span.end
	for insert > span.start && strings.TrimSpace(lines[insert-1]) == "" {
		insert--
	}
	lines = append(lines[:insert], append([]string{line}, lines[insert:]...)...)
	return joinLines(lines)
}

func readKey(text, table, key string) (string, bool) {
	lines := splitLines(text)
	span, found := locateTable(lines, table)
	if !found {
		return "", false
	}
	at := keyLine(lines, span, key)
	if at < 0 {
		return "", false
	}
	_, value, _ := strings.Cut(lines[at], "=")
	return strings.TrimSpace(value), true
}

func locateTable(lines []string, table string) (lineSpan, bool) {
	if table == "" {
		return lineSpan{0, nextHeader(lines, 0)}, true
	}
	for i, line := range lines {
		if strings.TrimSpace(line) == "["+table+"]" {
			return lineSpan{i + 1, nextHeader(lines, i+1)}, true
		}
	}
	return lineSpan{}, false
}

func nextHeader(lines []string, from int) int {
	for i := from; i < len(lines); i++ {
		if tableHeader.MatchString(lines[i]) {
			return i
		}
	}
	return len(lines)
}

func keyLine(lines []string, span lineSpan, key string) int {
	assignment := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)
	for i := span.start; i < span.end; i++ {
		if assignment.MatchString(lines[i]) {
			return i
		}
	}
	return -1
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func joinLines(lines []string) string { return strings.Join(lines, "\n") + "\n" }
