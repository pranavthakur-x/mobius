package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"mobius/pkg/llm"
)

// Decide asks the orchestrator model how to route one user message.
func (w *Workspace) Decide(ctx context.Context, userText string) (Decision, error) {
	catalog, err := json.Marshal(w.manager.CatalogSnapshot())
	if err != nil {
		return Decision{}, err
	}
	userJSON, err := json.Marshal(userText)
	if err != nil {
		return Decision{}, err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "<team_catalog>\n%s\n</team_catalog>\n", catalog)
	if recent := w.recentSnapshot(); len(recent) > 0 {
		recentJSON, err := json.Marshal(recent)
		if err != nil {
			return Decision{}, err
		}
		fmt.Fprintf(&sb, "<recent_turns>\n%s\n</recent_turns>\n", recentJSON)
	}
	fmt.Fprintf(&sb, "<user_message>\n%s\n</user_message>", userJSON)
	userContent := sb.String()

	req := &llm.ChatRequest{
		Model: w.model.Name,
		Message: []llm.Message{
			{Role: llm.RoleSystem, Content: orchestratorSystemPrompt},
			{Role: llm.RoleUser, Content: userContent},
		},
		Tools: nil,
	}

	resp, err := w.model.Provider.Generate(ctx, req)
	if err != nil {
		return Decision{}, err
	}
	if w.orchestrator != nil && w.orchestrator.Agent != nil {
		w.orchestrator.Agent.RecordUsage(resp.Usage)
	}
	dec, err := parseDecisionPayload(resp.Content)
	if err == nil {
		if err := validateDecision(dec, w.manager); err != nil {
			return Decision{}, err
		}
		return dec, nil
	}

	repairReq := &llm.ChatRequest{
		Model: w.model.Name,
		Message: append(append([]llm.Message(nil), req.Message...),
			llm.Message{Role: llm.RoleAssistant, Content: resp.Content},
			llm.Message{Role: llm.RoleUser, Content: orchestratorRepairPrompt},
		),
		Tools: nil,
	}
	resp2, err2 := w.model.Provider.Generate(ctx, repairReq)
	if err2 != nil {
		return Decision{}, fmt.Errorf("%w (repair call failed: %v)", err, err2)
	}
	if w.orchestrator != nil && w.orchestrator.Agent != nil {
		w.orchestrator.Agent.RecordUsage(resp2.Usage)
	}
	dec, err2 = parseDecisionPayload(resp2.Content)
	if err2 != nil {
		return Decision{}, fmt.Errorf("%w (after repair attempt)", err)
	}
	if err := validateDecision(dec, w.manager); err != nil {
		return Decision{}, err
	}
	return dec, nil
}

const orchestratorRepairPrompt = `Your previous response was not valid routing JSON. Respond with ONLY one JSON object (no prose) containing exactly these 6 string keys once each: think, action, reply, team_id, role_id, task.`

func parseDecisionPayload(raw string) (Decision, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Decision{}, fmt.Errorf("invalid routing JSON: orchestrator returned empty content")
	}
	body, err := stripOuterFence(raw)
	if err != nil {
		return Decision{}, err
	}
	body = strings.TrimSpace(extractJSONObject(body))
	if body == "" || !strings.HasPrefix(body, "{") {
		return Decision{}, fmt.Errorf("invalid routing JSON: no JSON object found in orchestrator response")
	}
	var m map[string]string
	dec := json.NewDecoder(strings.NewReader(body))
	if err := dec.Decode(&m); err != nil {
		return Decision{}, fmt.Errorf("invalid routing JSON: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return Decision{}, fmt.Errorf("trailing data after routing JSON")
	}
	required := []string{"think", "action", "reply", "team_id", "role_id", "task"}
	if len(m) != len(required) {
		return Decision{}, fmt.Errorf("routing JSON must contain exactly %d keys", len(required))
	}
	for _, k := range required {
		if _, ok := m[k]; !ok {
			return Decision{}, fmt.Errorf("missing key %q", k)
		}
	}
	for k, v := range m {
		if v != strings.TrimSpace(v) && k != "task" && k != "reply" && k != "think" {
			_ = v
		}
		if strings.Contains(k, "\n") {
			return Decision{}, fmt.Errorf("invalid key")
		}
	}
	return Decision{
		Think:  m["think"],
		Action: strings.ToLower(strings.TrimSpace(m["action"])),
		Reply:  m["reply"],
		TeamID: strings.TrimSpace(m["team_id"]),
		RoleID: strings.TrimSpace(m["role_id"]),
		Task:   strings.TrimSpace(m["task"]),
	}, nil
}

func stripOuterFence(s string) (string, error) {
	if !strings.HasPrefix(s, "```") {
		return s, nil
	}
	rest := strings.TrimPrefix(s[3:], "json")
	rest = strings.TrimPrefix(rest, "\n")
	rest = strings.TrimPrefix(rest, "\r\n")
	if !strings.HasSuffix(strings.TrimSpace(s), "```") {
		return "", fmt.Errorf("unclosed code fence")
	}
	end := strings.LastIndex(rest, "```")
	if end < 0 {
		return "", fmt.Errorf("unclosed code fence")
	}
	inner := strings.TrimSpace(rest[:end])
	if inner == "" {
		return "", fmt.Errorf("empty fenced payload")
	}
	return inner, nil
}

// extractJSONObject pulls the first balanced {...} object from prose or partial output.
func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	start := strings.Index(s, "{")
	if start < 0 {
		return s
	}
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return strings.TrimSpace(s)
}

func validateDecision(d Decision, mgr *Manager) error {
	switch d.Action {
	case "reply":
		if strings.TrimSpace(d.Reply) == "" {
			return fmt.Errorf("reply action requires non-empty reply")
		}
		if d.TeamID != "" || d.RoleID != "" || d.Task != "" {
			return fmt.Errorf("reply action requires empty team_id, role_id, and task")
		}
	case "delegate":
		if d.TeamID == "" || d.RoleID == "" || d.Task == "" {
			return fmt.Errorf("delegate action requires team_id, role_id, and task")
		}
		return mgr.ValidateTarget(d.TeamID, d.RoleID)
	default:
		return fmt.Errorf("unknown action %q", d.Action)
	}
	return nil
}

// DelegateAck returns the user-visible acknowledgment for a delegation.
func DelegateAck(teamID, roleID string) string {
	return fmt.Sprintf("Delegated to %s/%s.", teamID, roleID)
}
