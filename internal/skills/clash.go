package skills

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Clash is one name two installed skills answer to. Agents call a skill by its SKILL.md name and fall
// back to the folder, so whichever loads last wins and the other is unreachable.
type Clash struct {
	Name    string
	Folders []string
}

// Fix is the rename that ends the clash: the folders to give a new name, keeping the first.
func (c Clash) Fix() []string { return c.Folders[1:] }

// clashes groups the installed skills by the name agents would call them, and keeps the contested ones.
func clashes(store string, installed []string) []Clash {
	claims := map[string][]string{}
	for _, folder := range installed {
		claims[calledName(store, folder)] = append(claims[calledName(store, folder)], folder)
	}
	var found []Clash
	for name, folders := range claims {
		if len(folders) > 1 {
			slices.Sort(folders)
			found = append(found, Clash{Name: name, Folders: folders})
		}
	}
	slices.SortFunc(found, func(a, b Clash) int { return strings.Compare(a.Name, b.Name) })
	return found
}

// calledName is what an agent calls the skill in folder: its SKILL.md name, or the folder itself.
func calledName(store, folder string) string {
	text, _ := os.ReadFile(filepath.Join(store, folder, skillFile))
	if name := parseFrontmatter(string(text)).name; name != "" {
		return name
	}
	return folder
}
