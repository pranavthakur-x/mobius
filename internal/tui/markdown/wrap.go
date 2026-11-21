package markdown

import (
	"strings"
	"unicode/utf8"
)

// WrapRunes breaks text into lines at most width runes wide, preserving words when possible.
func WrapRunes(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		if para == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, wrapParagraph(para, width)...)
	}
	return lines
}

func wrapParagraph(para string, width int) []string {
	var lines []string
	words := strings.Fields(para)
	if len(words) == 0 {
		return []string{""}
	}
	var cur []rune
	for _, w := range words {
		wRunes := []rune(w)
		if len(wRunes) > width {
			if len(cur) > 0 {
				lines = append(lines, string(cur))
				cur = nil
			}
			for len(wRunes) > 0 {
				end := width
				if end > len(wRunes) {
					end = len(wRunes)
				}
				lines = append(lines, string(wRunes[:end]))
				wRunes = wRunes[end:]
			}
			continue
		}
		need := len(wRunes)
		if len(cur) > 0 {
			need++
		}
		if len(cur)+need > width {
			lines = append(lines, string(cur))
			cur = wRunes
		} else {
			if len(cur) > 0 {
				cur = append(cur, ' ')
			}
			cur = append(cur, wRunes...)
		}
	}
	if len(cur) > 0 {
		lines = append(lines, string(cur))
	}
	return lines
}

// VisibleWidth returns the display width in runes (not grapheme clusters).
func VisibleWidth(s string) int {
	return utf8.RuneCountInString(s)
}
