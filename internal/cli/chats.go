package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"mobius/pkg/events"
	"mobius/pkg/llm"
	"mobius/pkg/orchestrator"
	"mobius/pkg/roles"
	"mobius/pkg/sensors"
	"mobius/pkg/session"
	"mobius/pkg/tools"
)

// Boot opens saved chats from .mobius/chats, or starts one empty chat.
func Boot(ctx context.Context, roleStore *roles.Store, base *tools.Registry, model orchestrator.ModelSpec, factory orchestrator.AgentFactory, ev events.EventStore, cfg *llm.Config, workDir string) (*App, error) {
	dir := ".mobius/chats"
	_ = session.ImportEventChats(dir, ".mobius/events")
	records, active, err := session.LoadChats(dir)
	if err != nil {
		return nil, err
	}
	reg := orchestrator.NewWorkspaceRegistry()
	var manager *session.Manager
	var freshID string
	for _, rec := range records {
		ws, sess, err := newChatWorkspace(ctx, roleStore, base, model, factory, rec.Name, workDir)
		if err != nil {
			return nil, err
		}
		sess.ID = rec.ID
		sess.MarkStarted(rec.Name)
		replayTurns(sess, rec.Turns)
		if manager == nil {
			manager = session.NewManager(sess)
		} else {
			manager.AddSession(sess)
		}
		reg.Register(sess.ID, ws)
		if freshID == "" && len(rec.Turns) == 0 && strings.TrimSpace(rec.Name) == "New Chat" {
			freshID = rec.ID
		}
	}
	if manager == nil || freshID == "" {
		ws, sess, err := newChatWorkspace(ctx, roleStore, base, model, factory, "New Chat", workDir)
		if err != nil {
			return nil, err
		}
		if err := session.EnsureChat(dir, sess.ID, sess.Name()); err != nil {
			return nil, err
		}
		reg.Register(sess.ID, ws)
		if manager == nil {
			manager = session.NewManager(sess)
		} else {
			manager.AddSession(sess)
			_, _ = manager.SwitchSession(sess.ID)
		}
	} else if _, err := manager.SwitchSession(freshID); err != nil && active != "" {
		_, _ = manager.SwitchSession(active)
	}
	app := NewApp(manager, reg, roleStore, base, cfg, ev)
	if strings.TrimSpace(workDir) == "" {
		workDir = "."
	}
	app.WorkDir = workDir
	return app, nil
}

func newChatWorkspace(ctx context.Context, roleStore *roles.Store, base *tools.Registry, model orchestrator.ModelSpec, factory orchestrator.AgentFactory, name, workDir string) (*orchestrator.Workspace, *session.Session, error) {
	ws, err := orchestrator.NewWorkspace(ctx, name, roleStore, base, model, factory)
	if err != nil {
		return nil, nil, err
	}
	sess := ws.OrchestratorSession()
	if strings.TrimSpace(workDir) == "" {
		workDir = "."
	}
	if data, err := os.ReadFile(filepath.Join(workDir, "AGENTS.md")); err == nil {
		sess.Agent.SetSensors(sensors.NewRegistryFromGuide(workDir, string(data)))
	}
	if name == "" {
		name = "New Chat"
	}
	sess.MarkStarted(name)
	return ws, sess, nil
}

func replayTurns(sess *session.Session, turns []session.TranscriptTurn) {
	if sess == nil || sess.Context == nil {
		return
	}
	for _, turn := range turns {
		switch turn.Role {
		case "user":
			sess.Context.AddUserMessage(turn.Text)
		case "assistant":
			sess.Context.AddAssistantMessage(turn.Text, nil)
		}
	}
}

// RememberTurn appends one visible line and renames a generic chat from the first question.
func (a *App) RememberTurn(chatID, role, speaker, text string) {
	if a == nil || a.Sessions == nil || chatID == "" {
		return
	}
	name := ""
	if sess, err := a.Sessions.Get(chatID); err == nil {
		name = sess.Name()
	}
	updated, err := session.AppendTurn(a.ChatDir, chatID, name, session.TranscriptTurn{
		Role: role, Speaker: speaker, Text: text,
	})
	if err != nil || updated == "" || updated == name {
		return
	}
	if sess, err := a.Sessions.Get(chatID); err == nil {
		sess.MarkStarted(updated)
	}
}
