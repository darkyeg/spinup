package tailnet

import "regexp"

var machineName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidName reports whether name can be a machine's MagicDNS name.
func ValidName(name string) bool { return machineName.MatchString(name) }
