// Package redact scrubs secrets from text before it is shown or stored.
// It is a pure function of its input and keeps no shared buffers.
package redact

import (
	"regexp"
	"strings"
)

// Placeholder replaces a recognized secret.
const Placeholder = "[REDACTED]"

var (
	privateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	bearerPattern     = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9\-._~+/=]+`)
	openAIKeyPattern  = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{10,}\b`)
	googleKeyPattern  = regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{20,}\b`)
	awsKeyPattern     = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	assignmentStart   = regexp.MustCompile(`(?i)(?:'((?:[^']|'')*)'|(?:"([A-Za-z_][A-Za-z0-9_]*)")|([A-Za-z_][A-Za-z0-9_]*))(\s*[=:]\s*)`)
)

// Redact returns text with API keys, bearer tokens, private key blocks,
// and secret assignments replaced by Placeholder.
func Redact(text string) string {
	if text == "" {
		return text
	}
	out := privateKeyPattern.ReplaceAllString(text, Placeholder)
	out = bearerPattern.ReplaceAllString(out, Placeholder)
	out = openAIKeyPattern.ReplaceAllString(out, Placeholder)
	out = googleKeyPattern.ReplaceAllString(out, Placeholder)
	out = awsKeyPattern.ReplaceAllString(out, Placeholder)
	out = redactAssignments(out)
	return out
}

func redactAssignments(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = redactAssignmentsInLine(line)
	}
	return strings.Join(lines, "\n")
}

func redactAssignmentsInLine(line string) string {
	var out strings.Builder
	rest := line
	for rest != "" {
		idx := assignmentStart.FindStringSubmatchIndex(rest)
		if idx == nil {
			out.WriteString(rest)
			break
		}
		out.WriteString(rest[:idx[0]])
		prefix := rest[idx[0]:idx[1]]
		key := assignmentKey(rest, idx)
		tail := rest[idx[1]:]
		if !secretKey(key) {
			out.WriteString(prefix)
			rest = tail
			continue
		}
		out.WriteString(prefix)
		var consumed string
		consumed, rest = redactSecretValue(tail)
		out.WriteString(consumed)
	}
	return out.String()
}

func assignmentKey(rest string, idx []int) string {
	if idx[2] >= 0 && idx[3] > idx[2] {
		return unescapeSingleQuotedKey(rest[idx[2]:idx[3]])
	}
	if idx[4] >= 0 && idx[5] > idx[4] {
		return rest[idx[4]:idx[5]]
	}
	if idx[6] >= 0 && idx[7] > idx[6] {
		return rest[idx[6]:idx[7]]
	}
	return ""
}

func unescapeSingleQuotedKey(raw string) string {
	return strings.ReplaceAll(raw, "''", "'")
}

func redactSecretValue(tail string) (redacted string, rest string) {
	if tail == "" {
		return Placeholder, ""
	}
	switch tail[0] {
	case '"':
		end := strings.IndexByte(tail[1:], '"')
		if end < 0 {
			return Placeholder, ""
		}
		return Placeholder, tail[end+2:]
	case '\'':
		end := strings.IndexByte(tail[1:], '\'')
		if end < 0 {
			return Placeholder, ""
		}
		return Placeholder, tail[end+2:]
	default:
		return Placeholder, ""
	}
}

func secretKey(name string) bool {
	upper := strings.ToUpper(name)
	switch {
	case strings.HasSuffix(upper, "API_KEY"),
		strings.HasSuffix(upper, "SECRET_KEY"),
		strings.HasSuffix(upper, "ACCESS_KEY"),
		strings.HasSuffix(upper, "SECRET"),
		strings.HasSuffix(upper, "TOKEN"),
		strings.HasSuffix(upper, "PASSWORD"):
		return true
	default:
		return false
	}
}
