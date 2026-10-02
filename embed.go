// Package spinup holds files shared by the Python CLI and the Go service.
package spinup

import _ "embed"

// ProxyConfigTemplate is proxy/config.template.yaml, with {{HOST}}, {{PORT}}, {{API_KEY}} and
// {{SECRET_KEY}} placeholders.
//
//go:embed proxy/config.template.yaml
var ProxyConfigTemplate string
