package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

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

// RenderMarkdown formats Markdown text into width-constrained terminal lines
// styled with Mobius's lipgloss palette. Untrusted ANSI escapes are stripped first.
func RenderMarkdown(text string, width int) []string {
	text = StripANSI(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	if width < 10 {
		width = 10
	}

	rawLines := strings.Split(text, "\n")
	var out []string
	lastBlank := false

	appendBlank := func() {
		if len(out) > 0 && !lastBlank {
			out = append(out, "")
			lastBlank = true
		}
	}
	appendLines := func(lines ...string) {
		for _, ln := range lines {
			out = append(out, ln)
			lastBlank = ln == ""
		}
	}

	for i := 0; i < len(rawLines); {
		line := rawLines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			appendBlank()
			i++
			continue
		}

		// Fenced code block (``` or ~~~)
		if fence, lang, ok := parseFenceOpen(trimmed); ok {
			var codeLines []string
			i++
			for i < len(rawLines) {
				if isFenceClose(strings.TrimSpace(rawLines[i]), fence) {
					i++
					break
				}
				codeLines = append(codeLines, rawLines[i])
				i++
			}
			appendLines(renderCodeBlock(lang, codeLines, width)...)
			continue
		}

		// Horizontal rule
		if isHorizontalRule(trimmed) {
			ruleW := width
			if ruleW > 48 {
				ruleW = 48
			}
			appendLines(StyleBorder.Render(strings.Repeat("─", ruleW)))
			i++
			continue
		}

		// ATX Heading (#, ##, ###...)
		if level, heading, ok := parseHeading(trimmed); ok {
			appendLines(renderHeading(level, heading, width)...)
			i++
			continue
		}

		// Pipe table (header row + separator row)
		if i+1 < len(rawLines) && looksLikeTableRow(trimmed) && isTableSeparator(strings.TrimSpace(rawLines[i+1])) {
			start := i
			i += 2
			for i < len(rawLines) && looksLikeTableRow(strings.TrimSpace(rawLines[i])) {
				i++
			}
			appendLines(renderTable(rawLines[start:i], width)...)
			continue
		}

		// Blockquote (> ...)
		if strings.HasPrefix(trimmed, ">") {
			body := strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
			prefix := StyleBorder.Render("│ ")
			wrapped := wrapStyledSpans(parseInlineSpans(body), width-2, StyleQuote)
			if len(wrapped) == 0 {
				appendLines(prefix)
			} else {
				for _, ln := range wrapped {
					appendLines(prefix + ln)
				}
			}
			i++
			continue
		}

		// Unordered list item (-, *, +)
		if indent, body, ok := parseUnorderedItem(line); ok {
			level := indent / 2
			if level > 3 {
				level = 3
			}
			bullet := "• "
			if level > 0 {
				bullet = "◦ "
			}
			pad := strings.Repeat("  ", level)
			firstPrefix := pad + StyleBlue.Render(bullet)
			contPrefix := pad + "  "
			avail := width - (level*2 + 2)
			if avail < 8 {
				avail = 8
			}
			wrapped := wrapStyledSpans(parseInlineSpans(body), avail, lipgloss.NewStyle())
			if len(wrapped) == 0 {
				appendLines(firstPrefix)
			} else {
				for idx, ln := range wrapped {
					if idx == 0 {
						appendLines(firstPrefix + ln)
					} else {
						appendLines(contPrefix + ln)
					}
				}
			}
			i++
			continue
		}

		// Ordered list item (1. ...)
		if indent, num, body, ok := parseOrderedItem(line); ok {
			level := indent / 2
			if level > 3 {
				level = 3
			}
			pad := strings.Repeat("  ", level)
			marker := num + ". "
			markerW := utf8.RuneCountInString(marker)
			firstPrefix := pad + StyleBlue.Render(marker)
			contPrefix := pad + strings.Repeat(" ", markerW)
			avail := width - (level*2 + markerW)
			if avail < 8 {
				avail = 8
			}
			wrapped := wrapStyledSpans(parseInlineSpans(body), avail, lipgloss.NewStyle())
			if len(wrapped) == 0 {
				appendLines(firstPrefix)
			} else {
				for idx, ln := range wrapped {
					if idx == 0 {
						appendLines(firstPrefix + ln)
					} else {
						appendLines(contPrefix + ln)
					}
				}
			}
			i++
			continue
		}

		// Standard paragraph line
		wrapped := wrapStyledSpans(parseInlineSpans(trimmed), width, lipgloss.NewStyle())
		appendLines(wrapped...)
		i++
	}

	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func parseFenceOpen(trimmed string) (fence, lang string, ok bool) {
	if strings.HasPrefix(trimmed, "```") {
		n := countLeading(trimmed, '`')
		rest := strings.TrimSpace(trimmed[n:])
		if strings.ContainsRune(rest, '`') {
			return "", "", false
		}
		return strings.Repeat("`", n), rest, true
	}
	if strings.HasPrefix(trimmed, "~~~") {
		n := countLeading(trimmed, '~')
		rest := strings.TrimSpace(trimmed[n:])
		return strings.Repeat("~", n), rest, true
	}
	return "", "", false
}

func isFenceClose(trimmed, fence string) bool {
	if len(fence) == 0 {
		return false
	}
	ch := fence[0]
	n := countLeading(trimmed, ch)
	return n >= len(fence) && strings.TrimSpace(trimmed[n:]) == ""
}

func countLeading(s string, b byte) int {
	n := 0
	for n < len(s) && s[n] == b {
		n++
	}
	return n
}

func renderCodeBlock(lang string, lines []string, width int) []string {
	var out []string
	header := "┌"
	if lang != "" {
		header = "┌ " + lang
	}
	out = append(out, StyleMuted.Render(header))
	bar := StyleBorder.Render("│ ")
	innerW := width - 2
	if innerW < 8 {
		innerW = 8
	}
	for _, raw := range lines {
		expanded := strings.ReplaceAll(raw, "\t", "    ")
		if expanded == "" {
			out = append(out, bar)
			continue
		}
		runes := []rune(expanded)
		for len(runes) > 0 {
			end := innerW
			if end > len(runes) {
				end = len(runes)
			}
			out = append(out, bar+StyleCode.Render(string(runes[:end])))
			runes = runes[end:]
		}
	}
	out = append(out, StyleBorder.Render("└"))
	return out
}

func isHorizontalRule(trimmed string) bool {
	if len(trimmed) < 3 {
		return false
	}
	var ch rune
	count := 0
	for _, r := range trimmed {
		if r == ' ' {
			continue
		}
		if r != '-' && r != '*' && r != '_' {
			return false
		}
		if ch == 0 {
			ch = r
		} else if r != ch {
			return false
		}
		count++
	}
	return count >= 3
}

func parseHeading(trimmed string) (int, string, bool) {
	n := countLeading(trimmed, '#')
	if n < 1 || n > 6 || len(trimmed) <= n || trimmed[n] != ' ' {
		return 0, "", false
	}
	body := strings.TrimSpace(trimmed[n+1:])
	body = strings.TrimRight(body, "# ")
	if body == "" {
		return 0, "", false
	}
	return n, body, true
}

func renderHeading(level int, text string, width int) []string {
	style := StyleHeading3
	switch level {
	case 1:
		style = StyleHeading1
	case 2:
		style = StyleHeading2
	}
	lines := WrapRunes(stripInlineMarkers(text), width)
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		out = append(out, style.Render(ln))
	}
	return out
}

func parseUnorderedItem(line string) (indent int, body string, ok bool) {
	indent, rest := leadingSpaces(line)
	if len(rest) >= 2 && (rest[0] == '-' || rest[0] == '*' || rest[0] == '+') && rest[1] == ' ' {
		return indent, strings.TrimSpace(rest[2:]), true
	}
	return 0, "", false
}

func parseOrderedItem(line string) (indent int, num, body string, ok bool) {
	indent, rest := leadingSpaces(line)
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(rest) || rest[i] != '.' || rest[i+1] != ' ' {
		return 0, "", "", false
	}
	return indent, rest[:i], strings.TrimSpace(rest[i+2:]), true
}

func leadingSpaces(s string) (int, string) {
	n := 0
	i := 0
	for i < len(s) {
		switch s[i] {
		case ' ':
			n++
			i++
		case '\t':
			n += 2
			i++
		default:
			return n, s[i:]
		}
	}
	return n, ""
}

func looksLikeTableRow(trimmed string) bool {
	return strings.Contains(trimmed, "|")
}

func isTableSeparator(trimmed string) bool {
	if !strings.Contains(trimmed, "-") || !strings.Contains(trimmed, "|") {
		return false
	}
	cells := splitTableRow(trimmed)
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" {
			return false
		}
		dashes := 0
		for _, r := range c {
			if r == '-' {
				dashes++
			} else if r != ':' {
				return false
			}
		}
		if dashes == 0 {
			return false
		}
	}
	return true
}

func splitTableRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	parts := strings.Split(trimmed, "|")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}

func renderTable(rows []string, width int) []string {
	if len(rows) < 2 {
		return nil
	}
	header := splitTableRow(rows[0])
	cols := len(header)
	parsed := make([][]string, 0, len(rows)-1)
	parsed = append(parsed, header)
	for _, r := range rows[2:] {
		cells := splitTableRow(r)
		if len(cells) > cols {
			cols = len(cells)
		}
		parsed = append(parsed, cells)
	}
	colW := make([]int, cols)
	for _, r := range parsed {
		for c := 0; c < len(r); c++ {
			w := utf8.RuneCountInString(stripInlineMarkers(r[c]))
			if w > colW[c] {
				colW[c] = w
			}
		}
	}
	// Shrink columns proportionally if total exceeds available width.
	sepOverhead := (cols - 1) * 3
	avail := width - sepOverhead
	if avail < cols*4 {
		avail = cols * 4
	}
	total := 0
	for _, w := range colW {
		total += w
	}
	if total > avail && total > 0 {
		for c := range colW {
			w := (colW[c] * avail) / total
			if w < 4 {
				w = 4
			}
			colW[c] = w
		}
	}

	var out []string
	sepCell := StyleBorder.Render(" │ ")
	for rIdx, r := range parsed {
		var line strings.Builder
		for c := 0; c < cols; c++ {
			if c > 0 {
				line.WriteString(sepCell)
			}
			cell := ""
			if c < len(r) {
				cell = stripInlineMarkers(r[c])
			}
			cell = fitCell(cell, colW[c])
			if rIdx == 0 {
				line.WriteString(StyleBold.Render(cell))
			} else {
				line.WriteString(cell)
			}
		}
		out = append(out, line.String())
		if rIdx == 0 {
			var div strings.Builder
			for c := 0; c < cols; c++ {
				if c > 0 {
					div.WriteString("─┼─")
				}
				div.WriteString(strings.Repeat("─", colW[c]))
			}
			out = append(out, StyleBorder.Render(div.String()))
		}
	}
	return out
}

func fitCell(s string, width int) string {
	runes := []rune(s)
	if len(runes) > width {
		if width <= 1 {
			return string(runes[:width])
		}
		return string(runes[:width-1]) + "…"
	}
	if len(runes) < width {
		return s + strings.Repeat(" ", width-len(runes))
	}
	return s
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

		// Bold: **...** or __...__
		if strings.HasPrefix(s[i:], "**") {
			if end := strings.Index(s[i+2:], "**"); end > 0 {
				flush()
				spans = append(spans, mdSpan{text: s[i+2 : i+2+end], kind: spanBold})
				i += end + 4
				continue
			}
		}

		// Markdown link: [label](url)
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
		return StyleBold.Render(text)
	case spanItalic:
		return StyleItalic.Render(text)
	case spanCode:
		return StyleCode.Render(text)
	case spanMuted:
		return StyleMuted.Render(text)
	case spanBadgeRed:
		return StyleRed.Bold(true).Render(text)
	case spanBadgeOrange:
		return StyleOrange.Bold(true).Render(text)
	case spanBadgeBlue:
		return StyleBlue.Bold(true).Render(text)
	case spanBadgeGreen:
		return StyleGreen.Bold(true).Render(text)
	default:
		if base.Value() != "" || base.GetItalic() || base.GetBold() {
			return base.Render(text)
		}
		return text
	}
}
