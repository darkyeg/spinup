// Package tools checks that the dev tools in tools.json are installed and installs the missing ones.
package tools

import (
	"encoding/json"
	"fmt"
	"io/fs"

	"github.com/darkyeg/spinup/internal/source"
)

const file = "tools.json"

// Tool is one entry of tools.json: how to tell it is installed, and how each package manager installs it.
type Tool struct {
	Name       string   `json:"name"`
	Check      []string `json:"check"`
	Winget     string   `json:"winget"`
	Powershell string   `json:"powershell"`
	Brew       string   `json:"brew"`
	Apt        string   `json:"apt"`
	Script     string   `json:"script"`
	Npm        string   `json:"npm"`
	Nix        string   `json:"nix"`
}

// Load reads tools.json from the source.
func Load(src source.Source) ([]Tool, error) {
	data, err := fs.ReadFile(src.Data(), file)
	if err != nil {
		return nil, err
	}
	var doc struct{ Tools []Tool }
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return doc.Tools, nil
}
