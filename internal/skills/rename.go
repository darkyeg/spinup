package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Rename gives one of your own skills a new name, the one agents call it by, and reinstalls it.
// The folder keeps its name; only the name in its SKILL.md changes, so nothing moves or is lost.
func (m Manager) Rename(ctx context.Context, folder, name string) (Synced, error) {
	if err := checkNames([]string{name}); err != nil {
		return Synced{}, err
	}
	file := filepath.Join(m.ownPath(folder), skillFile)
	text, err := os.ReadFile(file)
	if err != nil {
		return Synced{}, fmt.Errorf("%s is not one of your own skills, so it can't be renamed here", folder)
	}
	if err := os.WriteFile(file, []byte(withName(string(text), name)), 0o644); err != nil {
		return Synced{}, err
	}
	return m.Sync(ctx)
}

// withName sets the name: line of a SKILL.md's frontmatter, adding the line or the frontmatter if absent.
func withName(text, name string) string {
	line := nameKey + " " + name
	if !strings.HasPrefix(text, "---") {
		return "---\n" + line + "\n---\n\n" + text
	}
	newline := "\n"
	if strings.Contains(text, "\r\n") {
		newline = "\r\n"
	}
	lines := strings.Split(text, newline)
	for i := 1; i < len(lines) && strings.TrimSpace(lines[i]) != "---"; i++ {
		if strings.HasPrefix(lines[i], nameKey) {
			lines[i] = line
			return strings.Join(lines, newline)
		}
	}
	return strings.Join(append([]string{lines[0], line}, lines[1:]...), newline)
}
