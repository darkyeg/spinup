// Package spinup is the data spinup applies to every machine; the binary carries a copy.
package spinup

import (
	"embed"
	"io/fs"
)

//go:embed agents tools.json skills/skills.json skills/per-repo.json proxy/config.template.yaml
var files embed.FS

// Files is the repo's data as built into this binary.
func Files() fs.FS { return files }

// ProxyConfigTemplate is proxy/config.template.yaml, with {{HOST}}, {{PORT}}, {{AUTH_DIR}}, {{API_KEY}} and
// {{SECRET_KEY}} placeholders.
//
//go:embed proxy/config.template.yaml
var ProxyConfigTemplate string
