package markdown

import (
	"strings"
	"unicode/utf8"

	"mobius/internal/tui/theme"

	"github.com/charmbracelet/lipgloss"
)

// Render formats Markdown text into width-constrained terminal lines
// styled with Mobius's lipgloss palette. Untrusted ANSI escapes are stripped first.
func Render(text string, width int) []string {
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
			appendLines(theme.StyleBorder.Render(strings.Repeat("─", ruleW)))
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
			prefix := theme.StyleBorder.Render("│ ")
			wrapped := wrapStyledSpans(parseInlineSpans(body), width-2, theme.StyleQuote)
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
			appendLines(renderListItem(pad+theme.StyleBlue.Render(bullet), pad+"  ", body, width-(level*2+2))...)
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
			appendLines(renderListItem(pad+theme.StyleBlue.Render(marker), pad+strings.Repeat(" ", markerW), body, width-(level*2+markerW))...)
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

func renderListItem(firstPrefix, contPrefix, body string, avail int) []string {
	if avail < 8 {
		avail = 8
	}
	wrapped := wrapStyledSpans(parseInlineSpans(body), avail, lipgloss.NewStyle())
	if len(wrapped) == 0 {
		return []string{firstPrefix}
	}
	out := make([]string, len(wrapped))
	for idx, ln := range wrapped {
		if idx == 0 {
			out[idx] = firstPrefix + ln
		} else {
			out[idx] = contPrefix + ln
		}
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
	out = append(out, theme.StyleMuted.Render(header))
	bar := theme.StyleBorder.Render("│ ")
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
			out = append(out, bar+theme.StyleCode.Render(string(runes[:end])))
			runes = runes[end:]
		}
	}
	out = append(out, theme.StyleBorder.Render("└"))
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
	style := theme.StyleHeading3
	switch level {
	case 1:
		style = theme.StyleHeading1
	case 2:
		style = theme.StyleHeading2
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
	sepCell := theme.StyleBorder.Render(" │ ")
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
				line.WriteString(theme.StyleBold.Render(cell))
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
			out = append(out, theme.StyleBorder.Render(div.String()))
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
