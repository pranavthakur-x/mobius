package artifact

import "fmt"

// Thresholds for tool output handling
const (
	SmallThreshold   = 2000 // Under 2k chars: keep full output in context
	PreviewThreshold = 8000 // 2k-8k chars: include a preview
	PreviewLines     = 20   // Number of lines to show in preview
)

// Result holds the processed tool output after interception.
type Result struct {
	Observation  string // What goes into the LLM context (full, preview, or ref)
	FullOutput   string // The complete original output
	ArtifactRef  string // artifact:// reference if offloaded, empty otherwise
	WasOffloaded bool
}

// Intercept processes tool output and decides whether to offload.
// view_file, list_dir, and grep_search stay in context: they are already
// capped by the tools themselves, and offloading them made the model
// re-read the same files with run_command because read_artifact does not exist.
func Intercept(store *Store, threadID, toolName, fullOutput string) Result {
	if keepToolOutputInContext(toolName) || len(fullOutput) <= SmallThreshold {
		return Result{
			Observation: fullOutput,
			FullOutput:  fullOutput,
		}
	}

	if store == nil {
		return Result{
			Observation: clipString(fullOutput, SmallThreshold) + "\n... [truncated]",
			FullOutput:  fullOutput,
		}
	}

	_, ref, err := store.Save(threadID, fullOutput)
	if err != nil {
		return Result{
			Observation: clipString(fullOutput, SmallThreshold) + "\n... [truncated]",
			FullOutput:  fullOutput,
		}
	}

	preview := buildPreview(fullOutput, PreviewLines)
	observation := fmt.Sprintf("Output saved to %s (%d bytes).\nPreview:\n%s\nThis output is too large for context. Call the same tool again with a narrower path, pattern, or command. Do not use run_command to cat this artifact.", ref, len(fullOutput), preview)

	return Result{
		Observation:  observation,
		FullOutput:   fullOutput,
		ArtifactRef:  ref,
		WasOffloaded: true,
	}
}

func keepToolOutputInContext(toolName string) bool {
	switch toolName {
	case "view_file", "list_dir", "grep_search":
		return true
	default:
		return false
	}
}

func clipString(s string, maxRunes int) string {
	if maxRunes <= 0 || s == "" {
		return ""
	}
	n := 0
	for i := range s {
		if n == maxRunes {
			return s[:i]
		}
		n++
	}
	return s
}

func buildPreview(content string, maxLines int) string {
	lines := splitLines(content)
	if len(lines) <= maxLines {
		return content
	}
	result := ""
	for i := 0; i < maxLines; i++ {
		result += lines[i] + "\n"
	}
	result += fmt.Sprintf("... (%d more lines)", len(lines)-maxLines)
	return result
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
