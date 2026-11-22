package orchestrator

import (
	"strings"
)

type commandKind int

const (
	cmdNone commandKind = iota
	cmdHelp
	cmdTeams
	cmdReloadRoles
	cmdMention
	cmdCreateTeam
	cmdTell
	cmdSteer
	cmdTeamLead
	cmdDecide
)

// HelpText returns formatted documentation of slash commands, mentions, and shortcuts.
func HelpText() string {
	return `# Mobius Commands & Shortcuts

### Slash Commands
• /help — Show this help message
• /teams — List active departments, teams, and member roles
• /reload-roles — Reload role definitions from disk
• /newchat — Start a fresh chat session
• /listchats — Switch between saved chats
• /models — Select or configure the active LLM
• /connect — Connect a custom LLM endpoint or API key
• /workdir — Change the active workspace directory
• /exit — Exit Mobius

### Role Delegation & Mentions
• *<role> <task> — Direct delegation to a role (e.g. *Software Engineer fix race bug)
• tell <team> <directive> — Send an in-flight steering directive to a team
• create team <dept> — Instantiate a new team from a department template

### Navigation & Shortcuts
• Tab / Shift+Tab — Switch focus between prompt and panels
• Ctrl+O — Open tools / file inspector
• Esc — Close open menu or clear input
• Ctrl+C — Cancel running turn or quit`
}

type parsedCommand struct {
	kind      commandKind
	mention   mentionMatch
	teamID    string
	roleID    string
	task      string
	directive string
	deptID    string
	raw       string
}

type mentionMatch struct {
	roleID   string
	roleName string
	task     string
}

// parseInput applies command precedence to user input.
func parseInput(input string, mgr *Manager) parsedCommand {
	trimmed := strings.TrimSpace(input)
	out := parsedCommand{raw: trimmed}
	if trimmed == "" {
		return out
	}
	lower := strings.ToLower(trimmed)

	switch lower {
	case "/help":
		out.kind = cmdHelp
		return out
	case "/teams":
		out.kind = cmdTeams
		return out
	case "/reload-roles":
		out.kind = cmdReloadRoles
		return out
	}

	if m, ok := parseMention(trimmed, mgr); ok {
		out.kind = cmdMention
		out.mention = m
		out.roleID = m.roleID
		out.task = m.task
		return out
	}

	if strings.HasPrefix(lower, "create team ") {
		dept := strings.TrimSpace(trimmed[len("create team "):])
		if dept != "" {
			out.kind = cmdCreateTeam
			out.deptID = dept
		}
		return out
	}

	if d, ok := parseTellSteer(trimmed, "tell"); ok {
		out.kind = cmdTell
		out.teamID = d.teamID
		out.directive = d.directive
		return out
	}
	if d, ok := parseTellSteer(trimmed, "steer"); ok {
		out.kind = cmdSteer
		out.teamID = d.teamID
		out.directive = d.directive
		return out
	}

	if teamID, task, ok := parseTeamLead(trimmed); ok {
		out.kind = cmdTeamLead
		out.teamID = teamID
		out.task = task
		return out
	}

	out.kind = cmdDecide
	return out
}

func parseMention(input string, mgr *Manager) (mentionMatch, bool) {
	if !strings.HasPrefix(input, "*") {
		return mentionMatch{}, false
	}
	rest := strings.TrimSpace(input[1:])
	if rest == "" {
		return mentionMatch{}, false
	}

	roles := mgr.storeRolesByLongestName()
	for _, r := range roles {
		if len(r.Name) > len(rest) {
			continue
		}
		prefix := rest[:len(r.Name)]
		if !strings.EqualFold(prefix, r.Name) {
			continue
		}
		after := rest[len(r.Name):]
		if len(after) == 0 || (after[0] != ' ' && after[0] != '\t') {
			continue
		}
		task := strings.TrimSpace(after)
		if task == "" {
			return mentionMatch{}, false
		}
		return mentionMatch{
			roleID:   r.ID,
			roleName: r.Name,
			task:     task,
		}, true
	}
	return mentionMatch{}, false
}

func (m *Manager) storeRolesByLongestName() []struct {
	ID   string
	Name string
} {
	all := m.store.Roles()
	type pair struct {
		ID   string
		Name string
	}
	out := make([]pair, 0, len(all))
	for _, r := range all {
		out = append(out, pair{ID: r.ID, Name: r.Name})
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if len(out[j].Name) > len(out[i].Name) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	result := make([]struct {
		ID   string
		Name string
	}, len(out))
	for i, p := range out {
		result[i].ID = p.ID
		result[i].Name = p.Name
	}
	return result
}

type directiveParse struct {
	teamID    string
	directive string
}

func parseTellSteer(input, verb string) (directiveParse, bool) {
	lower := strings.ToLower(input)
	prefix := verb + " "
	if !strings.HasPrefix(lower, prefix) {
		return directiveParse{}, false
	}
	rest := strings.TrimSpace(input[len(prefix):])
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		return directiveParse{}, false
	}
	teamID := parts[0]
	dirWords := parts[1:]
	if len(dirWords) >= 2 && strings.EqualFold(dirWords[0], "to") {
		dirWords = dirWords[1:]
	}
	if len(dirWords) == 0 {
		return directiveParse{}, false
	}
	return directiveParse{
		teamID:    teamID,
		directive: strings.Join(dirWords, " "),
	}, true
}

func parseTeamLead(input string) (teamID, task string, ok bool) {
	lower := strings.ToLower(input)
	if !strings.HasPrefix(lower, "team ") {
		return "", "", false
	}
	rest := strings.TrimSpace(input[len("team "):])
	colon := strings.Index(rest, ":")
	if colon <= 0 {
		return "", "", false
	}
	teamID = strings.TrimSpace(rest[:colon])
	task = strings.TrimSpace(rest[colon+1:])
	if teamID == "" || task == "" {
		return "", "", false
	}
	return teamID, task, true
}
