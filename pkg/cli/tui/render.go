package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderSidebar(w, height int, bd ContextBreakdown, ledger BudgetSnapshot, teams []TeamLine) string {
	inner := w - 2
	if inner < 10 {
		inner = 10
	}
	var b strings.Builder
	b.WriteString(StyleMuted.Render("Context"))
	b.WriteString("\n\n")
	barWidth := inner
	if bd.HasPrice {
		cost := bd.CostUSD
		if ledger.CostUSD > cost {
			cost = ledger.CostUSD
		}
		b.WriteString(StyleGreen.Render(fmt.Sprintf("$%.5f", cost)))
		b.WriteString("  ")
		barWidth = inner - 12
	}
	b.WriteString(renderUsageBar(bd, barWidth))
	b.WriteString("\n")
	pct := UsagePercent(bd)
	b.WriteString(StyleMuted.Render(fmt.Sprintf("%d%%  ~%d / 128.0K", pct, bd.Used)))
	b.WriteString("\n\n")
	b.WriteString(legendLine("■", StyleBlue, "System prompt", bd.System))
	b.WriteString("\n")
	b.WriteString(legendLine("■", StyleGreen, "Tool definitions", bd.ToolDefs))
	b.WriteString("\n")
	b.WriteString(legendLine("■", StylePurple, "Rules", bd.Rules))
	b.WriteString("\n")
	b.WriteString(legendLine("■", StyleOrange, "Conversation", bd.Conversation))
	b.WriteString("\n\n")
	b.WriteString(StyleMuted.Render("Teams"))
	b.WriteString("\n")
	lastDept := ""
	for _, t := range teams {
		if t.DeptName != lastDept {
			if lastDept != "" {
				b.WriteString("\n")
			}
			b.WriteString(StylePurple.Render(t.DeptName))
			b.WriteString("\n")
			lastDept = t.DeptName
		}
		b.WriteString("  ")
		b.WriteString(t.Name)
		b.WriteString(" · ")
		b.WriteString(StyleMuted.Render(t.State.String()))
		b.WriteString("\n")
	}
	if height < 1 {
		height = 1
	}
	return StyleSidebar.Width(w).Height(height).MaxHeight(height).Render(strings.TrimRight(b.String(), "\n"))
}

func legendLine(sym string, sty lipgloss.Style, label string, n int) string {
	return sty.Render(sym) + " " + label + "  " + StyleMuted.Render(fmt.Sprintf("%d", n))
}

func renderUsageBar(bd ContextBreakdown, width int) string {
	if width < 4 {
		width = 4
	}
	cap := bd.Capacity
	if cap <= 0 {
		cap = TokenCapacity()
	}
	used := bd.Used
	if used > cap {
		used = cap
	}
	filled := (width * used) / cap
	if used > 0 && filled == 0 {
		filled = 1
	}
	track := width
	var bar strings.Builder
	segs := UsageBarSegments(bd, width)
	pos := 0
	for _, sg := range segs {
		if sg.Count <= 0 {
			continue
		}
		bar.WriteString(segmentStyle(sg.Style).Render(strings.Repeat("█", sg.Count)))
		pos += sg.Count
	}
	for pos < filled {
		bar.WriteString(StyleGreen.Render("█"))
		pos++
	}
	for pos < track {
		bar.WriteString(StyleDim.Render("░"))
		pos++
	}
	return bar.String()
}

func renderTranscriptLine(bl TranscriptBlock, chatWidth int, showSteps bool) []string {
	switch bl.Kind {
	case BlockUser:
		line := StyleUser.Width(chatWidth).Render("❯ " + bl.Text)
		return []string{line, ""}
	case BlockFinal:
		dot := roleDot(bl.RoleID, bl.Speaker)
		head := dot + " " + StyleMuted.Render(bl.Speaker)
		body := RenderMarkdown(bl.Text, chatWidth-2)
		var out []string
		out = append(out, head)
		for _, ln := range body {
			out = append(out, "  "+ln)
		}
		out = append(out, "")
		return out
	case BlockTrace:
		return activityLines(bl.Speaker, bl.RoleID, bl.Text, chatWidth)
	case BlockError:
		return []string{StyleRed.Render(bl.Text), ""}
	case BlockThought:
		return nil
	case BlockToolGroup:
		who := activitySpeaker(bl.Speaker)
		if bl.Expanded {
			var out []string
			for _, t := range bl.Tools {
				out = append(out, activityLines(who, bl.RoleID, toolActivity(t.Name, t.Args), chatWidth)...)
			}
			return out
		}
		action := "called tools"
		if bl.ToolCount == 1 && len(bl.Tools) == 1 {
			action = toolActivity(bl.Tools[0].Name, bl.Tools[0].Args)
		} else if bl.ToolCount > 0 {
			action = fmt.Sprintf("called %d tools", bl.ToolCount)
		}
		line := activityLines(who, bl.RoleID, action+"  (ctrl+o to expand)", chatWidth)
		return line
	default:
		if !showSteps {
			return nil
		}
		return activityLines(bl.Speaker, bl.RoleID, bl.Text, chatWidth)
	}
}

func activitySpeaker(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "agent"
	}
	return name
}

func activityLines(speaker, roleID, action string, width int) []string {
	_ = roleID
	who := activitySpeaker(speaker)
	action = strings.TrimSpace(action)
	head := StyleTrace.Render("● " + who)
	if action == "" {
		return []string{head, ""}
	}
	body := WrapRunes(action, width-2)
	out := []string{head}
	for _, ln := range body {
		out = append(out, StyleTrace.Render("  "+ln))
	}
	out = append(out, "")
	return out
}

func toolActivity(name, args string) string {
	name = strings.TrimSpace(name)
	args = strings.TrimSpace(args)
	if args == "" || args == "{}" {
		return name
	}
	if len(args) > 80 {
		return name
	}
	return name + " " + args
}

func roleDot(roleID, speaker string) string {
	switch {
	case speaker == "orchestrator" || roleID == "":
		return StyleGreen.Render("●")
	case strings.Contains(strings.ToLower(speaker), "review"):
		return StylePurple.Render("●")
	default:
		return StyleBlue.Render("●")
	}
}

func flattenTranscript(blocks []TranscriptBlock, width int, showSteps bool) []string {
	var lines []string
	for _, bl := range blocks {
		lines = append(lines, renderTranscriptLine(bl, width, showSteps)...)
	}
	return lines
}
