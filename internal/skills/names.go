package skills

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

var (
	nonportableChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
	windowsDevice    = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[1-9¹²³]|LPT[1-9¹²³]|CONIN\$|CONOUT\$)( *\.|$)`)
)

func checkNames(names []string) error {
	for _, name := range names {
		if !fs.ValidPath(name) || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") ||
			nonportableChars.MatchString(name) || windowsDevice.MatchString(name) {
			return fmt.Errorf("%q isn't a portable skill folder name", name)
		}
	}
	return nil
}
