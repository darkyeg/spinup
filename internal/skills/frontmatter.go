package skills

import (
	"strings"
	"unicode"
)

const (
	nameKey          = "name:"
	descriptionKey   = "description:"
	authorManualFlag = "disable-model-invocation: true"
	descriptionNoise = ">|\"'"
)

type frontmatter struct {
	name         string
	description  string
	authorManual bool
}

func parseFrontmatter(text string) frontmatter {
	if !strings.HasPrefix(text, "---") {
		return frontmatter{}
	}
	head := strings.Split(text, "---")[1]
	var parts []string
	name := ""
	inDescription := false
	for _, line := range strings.Split(head, "\n") {
		line = strings.TrimRight(line, "\r")
		if rest, ok := strings.CutPrefix(line, nameKey); ok {
			name = strings.Trim(strings.TrimSpace(rest), descriptionNoise)
		}
		if rest, ok := strings.CutPrefix(line, descriptionKey); ok {
			inDescription, line = true, rest
		} else if line != "" && !unicode.IsSpace([]rune(line)[0]) {
			inDescription = false
		}
		if part := strings.Trim(strings.TrimSpace(line), descriptionNoise); inDescription && part != "" {
			parts = append(parts, part)
		}
	}
	return frontmatter{name, strings.Join(parts, " "), strings.Contains(head, authorManualFlag)}
}
