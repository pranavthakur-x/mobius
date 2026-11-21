package tui

import (
	"regexp"
	"strings"

	"mobius/internal/tui/markdown"
)

var (
	budgetRE = regexp.MustCompile(`^\[Budget\] Tokens: (\d+) In, (\d+) Out(?: \| Cost: \$([0-9.]+) \(₹([0-9.]+)\))?`)
	stepRE   = regexp.MustCompile(`^\[Step [^\]]+\]`)
)

// LineDecoder converts agent line-hook strings into typed live events.
type LineDecoder struct{}

func NewLineDecoder() *LineDecoder {
	return &LineDecoder{}
}

// Decode classifies one complete emitLine chunk.
func (d *LineDecoder) Decode(src SourceRef, text string) []LiveEvent {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if strings.HasPrefix(text, "[Goal]") {
		return nil
	}
	if m := budgetRE.FindStringSubmatch(firstLine(text)); len(m) == 5 {
		in, out := atoi(m[1]), atoi(m[2])
		usd, inr := 0.0, 0.0
		if m[3] != "" {
			usd, inr = atof(m[3]), atof(m[4])
		}
		return []LiveEvent{BudgetEvent{
			SourceRef: src,
			InTokens:  in,
			OutTokens: out,
			CostUSD:   usd,
			CostINR:   inr,
			Raw:       text,
		}}
	}
	if stepRE.MatchString(firstLine(text)) {
		body := strings.TrimSpace(stepRE.ReplaceAllString(firstLine(text), ""))
		lower := strings.ToLower(body)
		switch {
		case strings.Contains(lower, "thinking"):
			return nil
		case strings.Contains(lower, "summar"):
			return []LiveEvent{AgentStepEvent{SourceRef: src, Text: "summarizing the conversation"}}
		case body == "":
			return nil
		default:
			return []LiveEvent{AgentStepEvent{SourceRef: src, Text: strings.Trim(body, ". ")}}
		}
	}
	if strings.HasPrefix(text, "[Thought]") {
		body := strings.TrimPrefix(text, "[Thought]")
		body = strings.TrimLeft(body, "\n")
		body = strings.TrimSpace(body)
		return []LiveEvent{AgentThoughtEvent{SourceRef: src, Text: markdown.StripANSI(body)}}
	}
	if strings.HasPrefix(text, "[Tool]") {
		rest := strings.TrimPrefix(text, "[Tool]")
		rest = strings.TrimSpace(rest)
		name, args := splitTool(rest)
		return []LiveEvent{AgentToolEvent{SourceRef: src, Name: name, Args: args}}
	}
	if strings.HasPrefix(text, "[Sensor]") {
		action := strings.TrimSpace(strings.TrimPrefix(text, "[Sensor]"))
		action = strings.Trim(markdown.StripANSI(action), ". ")
		action = strings.ToLower(action)
		if action == "" {
			action = "running checks"
		}
		return []LiveEvent{AgentSensorEvent{SourceRef: src, Text: action}}
	}
	// Verification / warning lines from sensors often lack a strict prefix.
	if strings.Contains(text, "verification") || strings.Contains(text, "WARNING") {
		return []LiveEvent{AgentSensorEvent{SourceRef: src, Text: markdown.StripANSI(text)}}
	}
	return []LiveEvent{AgentStepEvent{SourceRef: src, Text: markdown.StripANSI(text)}}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func splitTool(s string) (name, args string) {
	i := strings.IndexByte(s, '(')
	if i < 0 {
		return strings.TrimSpace(s), ""
	}
	name = strings.TrimSpace(s[:i])
	args = strings.TrimSuffix(strings.TrimSpace(s[i+1:]), ")")
	return name, args
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func atof(s string) float64 {
	var f float64
	var frac float64
	var div float64 = 1
	seenDot := false
	for _, c := range s {
		if c == '.' {
			seenDot = true
			continue
		}
		if c < '0' || c > '9' {
			break
		}
		if seenDot {
			div *= 10
			frac = frac*10 + float64(c-'0')
		} else {
			f = f*10 + float64(c-'0')
		}
	}
	return f + frac/div
}
