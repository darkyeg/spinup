package agentconfig

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	integerValue = regexp.MustCompile(`^[+-]?\d+$`)
	floatValue   = regexp.MustCompile(`^[+-]?\d+(\.\d+)?([eE][+-]?\d+)?$`)
	tableName    = regexp.MustCompile(`^\[([A-Za-z0-9_-]+)\]$`)
	keyName      = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

type tomlSetting struct {
	table string
	key   string
	value string
}

func parseWanted(text string) ([]tomlSetting, error) {
	var settings []tomlSetting
	table := ""
	for number, raw := range splitLines(strings.ReplaceAll(text, "\r\n", "\n")) {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if header := tableName.FindStringSubmatch(line); header != nil {
			table = header[1]
			continue
		}
		key, rawValue, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !keyName.MatchString(key) {
			return nil, fmt.Errorf("config.toml line %d: only [table] headers and key = value lines are supported", number+1)
		}
		value, err := parseScalar(strings.TrimSpace(rawValue))
		if err != nil {
			return nil, fmt.Errorf("config.toml line %d: %w", number+1, err)
		}
		settings = append(settings, tomlSetting{table: table, key: key, value: value})
	}
	return settings, nil
}

func parseScalar(raw string) (string, error) {
	switch {
	case raw == "true" || raw == "false":
		return literal(raw == "true")
	case integerValue.MatchString(raw):
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return "", err
		}
		return literal(n)
	case floatValue.MatchString(raw):
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return "", err
		}
		return literal(f)
	case strings.HasPrefix(raw, `"`):
		var text string
		if err := json.Unmarshal([]byte(raw), &text); err != nil {
			return "", fmt.Errorf("unsupported string %s", raw)
		}
		return literal(text)
	}
	return "", fmt.Errorf("unsupported value %q: only bool, int, float and basic string", raw)
}
