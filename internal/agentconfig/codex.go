package agentconfig

import "fmt"

func applyCodexSettings(current, wanted string) (string, error) {
	settings, err := parseWanted(wanted)
	if err != nil {
		return "", err
	}
	edited := current
	for _, s := range settings {
		edited = setKey(edited, s.table, s.key, s.value)
	}
	for _, s := range settings {
		if got, ok := readKey(edited, s.table, s.key); !ok || got != s.value {
			return "", fmt.Errorf("config.toml: %s.%s does not read back after editing; not writing it", s.table, s.key)
		}
	}
	return edited, nil
}
