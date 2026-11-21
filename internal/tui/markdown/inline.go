package markdown

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"mobius/internal/tui/theme"

	"github.com/charmbracelet/lipgloss"
)

type spanKind int

const (
	spanPlain spanKind = iota
	spanBold
	spanItalic
	spanCode
	spanMuted
	spanBadgeRed
	spanBadgeOrange
	spanBadgeBlue
	spanBadgeGreen
)

type mdSpan struct {
	text string
	kind spanKind
}

func stripInlineMarkers(s string) string {
	spans := parseInlineSpans(s)
	var b strings.Builder
	for _, sp := range spans {
		b.WriteString(sp.text)
	}
	return b.String()
}

// parseInlineSpans parses inline code, bold, italic, links, and priority/verdict badges.
func parseInlineSpans(s string) []mdSpan {
	var spans []mdSpan
	var plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			spans = append(spans, mdSpan{text: plain.String(), kind: spanPlain})
			plain.Reset()
		}
	}

	i := 0
	for i < len(s) {
		// Inline code: `...`
		if s[i] == '`' {
			if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
				flush()
				code := s[i+1 : i+1+end]
				if code != "" {
					spans = append(spans, mdSpan{text: code, kind: spanCode})
				}
				i += end + 2
				continue
			}
		}

		// Bold: **...**
		if strings.HasPrefix(s[i:], "**") {
			if end := strings.Index(s[i+2:], "**"); end > 0 {
				flush()
				spans = append(spans, mdSpan{text: s[i+2 : i+2+end], kind: spanBold})
				i += end + 4
				continue
			}
		}

		// Markdown link: [label](url) or priority badge: [P0]/[P1]/[P2]/[BLOCKER]
		if s[i] == '[' {
			if badge, kind, n, ok := matchPriorityBadge(s[i:]); ok {
				flush()
				spans = append(spans, mdSpan{text: badge, kind: kind})
				i += n
				continue
			}
			if closeBracket := strings.IndexByte(s[i+1:], ']'); closeBracket > 0 {
				after := i + 1 + closeBracket + 1
				if after < len(s) && s[after] == '(' {
					if closeParen := strings.IndexByte(s[after+1:], ')'); closeParen >= 0 {
						label := s[i+1 : i+1+closeBracket]
						url := s[after+1 : after+1+closeParen]
						flush()
						spans = append(spans, mdSpan{text: label, kind: spanBold})
						if url != "" && !strings.HasPrefix(url, "file://") {
							spans = append(spans, mdSpan{text: " (" + url + ")", kind: spanMuted})
						}
						i = after + 1 + closeParen + 1
						continue
					}
				}
			}
		}

		// Italic: *...* (single asterisk, non-space after opening)
		if s[i] == '*' && !strings.HasPrefix(s[i:], "**") && i+1 < len(s) && s[i+1] != ' ' {
			if end := strings.IndexByte(s[i+1:], '*'); end > 0 && s[i+end] != ' ' {
				flush()
				spans = append(spans, mdSpan{text: s[i+1 : i+1+end], kind: spanItalic})
				i += end + 2
				continue
			}
		}

		// Verdict keywords at word boundary
		if verdict, kind, n, ok := matchVerdictToken(s, i); ok {
			flush()
			spans = append(spans, mdSpan{text: verdict, kind: kind})
			i += n
			continue
		}

		r, size := utf8.DecodeRuneInString(s[i:])
		plain.WriteRune(r)
		i += size
	}
	flush()
	return spans
}

func matchPriorityBadge(rest string) (string, spanKind, int, bool) {
	switch {
	case strings.HasPrefix(rest, "[P0]"):
		return "[P0]", spanBadgeRed, 4, true
	case strings.HasPrefix(rest, "[P1]"):
		return "[P1]", spanBadgeOrange, 4, true
	case strings.HasPrefix(rest, "[P2]"):
		return "[P2]", spanBadgeBlue, 4, true
	case strings.HasPrefix(rest, "[BLOCKER]"):
		return "[BLOCKER]", spanBadgeRed, 9, true
	default:
		return "", spanPlain, 0, false
	}
}

func matchVerdictToken(s string, i int) (string, spanKind, int, bool) {
	if i > 0 {
		prev, _ := utf8.DecodeLastRuneInString(s[:i])
		if unicode.IsLetter(prev) || unicode.IsDigit(prev) || prev == '_' {
			return "", spanPlain, 0, false
		}
	}
	rest := s[i:]
	candidates := []struct {
		tok  string
		kind spanKind
	}{
		{"REQUEST CHANGES", spanBadgeRed},
		{"APPROVE WITH NOTES", spanBadgeOrange},
		{"APPROVE", spanBadgeGreen},
	}
	for _, c := range candidates {
		if strings.HasPrefix(rest, c.tok) {
			after := i + len(c.tok)
			if after < len(s) {
				next, _ := utf8.DecodeRuneInString(s[after:])
				if unicode.IsLetter(next) || unicode.IsDigit(next) || next == '_' {
					continue
				}
			}
			return c.tok, c.kind, len(c.tok), true
		}
	}
	return "", spanPlain, 0, false
}

type styledToken struct {
	text  string
	runes int
	kind  spanKind
	space bool
}

func wrapStyledSpans(spans []mdSpan, width int, base lipgloss.Style) []string {
	if width <= 0 {
		width = 80
	}
	var tokens []styledToken
	for _, sp := range spans {
		tokens = append(tokens, tokenizeSpan(sp)...)
	}
	if len(tokens) == 0 {
		return nil
	}

	var lines []string
	var cur strings.Builder
	curW := 0
	pendingSpace := false

	flushLine := func() {
		if cur.Len() > 0 {
			lines = append(lines, cur.String())
			cur.Reset()
			curW = 0
		}
		pendingSpace = false
	}

	for _, tok := range tokens {
		if tok.space {
			if curW > 0 {
				pendingSpace = true
			}
			continue
		}

		if tok.runes > width {
			flushLine()
			runes := []rune(tok.text)
			for len(runes) > 0 {
				end := width
				if end > len(runes) {
					end = len(runes)
				}
				lines = append(lines, renderToken(string(runes[:end]), tok.kind, base))
				runes = runes[end:]
			}
			continue
		}

		need := tok.runes
		if pendingSpace && curW > 0 {
			need++
		}
		if curW+need > width && curW > 0 {
			flushLine()
		}
		if pendingSpace && curW > 0 {
			cur.WriteString(renderToken(" ", spanPlain, base))
			curW++
		}
		pendingSpace = false
		cur.WriteString(renderToken(tok.text, tok.kind, base))
		curW += tok.runes
	}
	flushLine()
	return lines
}

func tokenizeSpan(sp mdSpan) []styledToken {
	var out []styledToken
	var word strings.Builder
	wordRunes := 0
	flushWord := func() {
		if wordRunes > 0 {
			out = append(out, styledToken{text: word.String(), runes: wordRunes, kind: sp.kind})
			word.Reset()
			wordRunes = 0
		}
	}

	inSpace := false
	for _, r := range sp.text {
		if unicode.IsSpace(r) {
			flushWord()
			if !inSpace {
				out = append(out, styledToken{text: " ", runes: 1, kind: spanPlain, space: true})
				inSpace = true
			}
			continue
		}
		inSpace = false
		word.WriteRune(r)
		wordRunes++
	}
	flushWord()
	return out
}

func renderToken(text string, kind spanKind, base lipgloss.Style) string {
	switch kind {
	case spanBold:
		return theme.StyleBold.Render(text)
	case spanItalic:
		return theme.StyleItalic.Render(text)
	case spanCode:
		return theme.StyleCode.Render(text)
	case spanMuted:
		return theme.StyleMuted.Render(text)
	case spanBadgeRed:
		return theme.StyleRed.Bold(true).Render(text)
	case spanBadgeOrange:
		return theme.StyleOrange.Bold(true).Render(text)
	case spanBadgeBlue:
		return theme.StyleBlue.Bold(true).Render(text)
	case spanBadgeGreen:
		return theme.StyleGreen.Bold(true).Render(text)
	default:
		if base.Value() != "" || base.GetItalic() || base.GetBold() {
			return base.Render(text)
		}
		return text
	}
}
