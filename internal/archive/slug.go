package archive

import "strings"

const (
	maxSlugLength = 80
	fallbackSlug  = "shortcut"
	lastASCII     = 0x7F
)

func Slug(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	pendingSeparator := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if pendingSeparator && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingSeparator = false
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			if pendingSeparator && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingSeparator = false
			b.WriteRune(r + ('a' - 'A'))
		case r > lastASCII:
			continue
		default:
			pendingSeparator = true
		}
	}

	s := b.String()
	if len(s) > maxSlugLength {
		s = strings.TrimRight(s[:maxSlugLength], "-")
	}
	if s == "" {
		return fallbackSlug
	}
	return s
}
