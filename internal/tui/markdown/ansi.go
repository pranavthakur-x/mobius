package markdown

import (
	"strings"
	"unicode/utf8"
)

// StripANSI removes ANSI CSI/OSC sequences and stray escapes from untrusted hook output.
func StripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			if i+1 < len(s) && s[i+1] == '[' {
				j := i + 2
				for j < len(s) && s[j] != 'm' {
					j++
				}
				if j < len(s) {
					i = j + 1
					continue
				}
			}
			if i+1 < len(s) && s[i+1] == ']' {
				j := i + 2
				for j < len(s) && s[j] != '\x07' && s[j] != '\x1b' {
					j++
				}
				if j < len(s) {
					if s[j] == '\x07' {
						i = j + 1
					} else {
						i = j
					}
					continue
				}
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r != utf8.RuneError {
			b.WriteRune(r)
		}
		i += size
	}
	return b.String()
}
