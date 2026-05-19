package filename

import (
	"net/url"
	"path"
	"strings"
)

const DefaultOutputFilename = "download.bin"

func DeriveSafeOutputFilenameFromURL(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return DefaultOutputFilename
	}

	base := path.Base(strings.TrimSpace(u.Path))
	if base == "" || base == "." || base == "/" {
		return DefaultOutputFilename
	}

	if decoded, err := url.PathUnescape(base); err == nil {
		base = decoded
	}

	base = SanitizeOutputFilename(base)
	if base == "" {
		return DefaultOutputFilename
	}

	return base
}

func SanitizeOutputFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r < 32:
			continue
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}

	out := strings.TrimSpace(strings.Trim(b.String(), ". "))
	if out == "" {
		return ""
	}

	upper := strings.ToUpper(out)
	switch upper {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return "_" + out
	}

	return out
}
