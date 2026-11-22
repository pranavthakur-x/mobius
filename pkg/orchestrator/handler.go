package orchestrator

import (
	"context"
	"fmt"
	"strings"
)

// Handle routes user input through commands, mentions, or orchestrator decide.
func (w *Workspace) Handle(ctx context.Context, input string, mode DispatchMode) (DispatchResult, error) {
	res, err := w.route(ctx, input, mode)
	if err == nil {
		w.rememberTurn(input, res.Reply)
	}
	return res, err
}

func (w *Workspace) rememberTurn(user, assistant string) {
	if strings.TrimSpace(user) != "" {
		if w.orchestrator != nil && w.orchestrator.Context != nil {
			w.orchestrator.Context.AddUserMessage(user)
		}
		w.recordRecent("user", user)
	}
	if strings.TrimSpace(assistant) != "" {
		if w.orchestrator != nil && w.orchestrator.Context != nil {
			w.orchestrator.Context.AddAssistantMessage(assistant, nil)
		}
		w.recordRecent("orchestrator", assistant)
	}
}

func (w *Workspace) route(ctx context.Context, input string, mode DispatchMode) (DispatchResult, error) {
	pc := parseInput(input, w.manager)

	switch pc.kind {
	case cmdHelp:
		return DispatchResult{Kind: KindCommand, Reply: HelpText()}, nil
	case cmdTeams:
		return DispatchResult{Kind: KindCommand, Reply: w.manager.FormatTeamsList()}, nil
	case cmdReloadRoles:
		if err := w.manager.ReloadRoles(ctx); err != nil {
			return DispatchResult{}, err
		}
		return DispatchResult{Kind: KindCommand, Reply: "Roles reloaded."}, nil
	case cmdCreateTeam:
		if err := w.manager.CreateTeam(pc.deptID); err != nil {
			return DispatchResult{}, err
		}
		return DispatchResult{Kind: KindCommand, Reply: fmt.Sprintf("Created team %q.", pc.deptID)}, nil
	case cmdTell, cmdSteer:
		running, err := w.manager.AddDirective(pc.teamID, pc.directive)
		if err != nil {
			return DispatchResult{}, err
		}
		note := fmt.Sprintf("[Team directive] %s", pc.directive)
		for _, worker := range running {
			worker.Session.Agent.PushNote(note)
		}
		return DispatchResult{Kind: KindCommand, Reply: fmt.Sprintf("Recorded directive for team %q.", pc.teamID)}, nil
	case cmdMention:
		teamID, ok := w.manager.TeamForRole(pc.roleID)
		if !ok {
			return DispatchResult{}, fmt.Errorf("role %q is not on any team", pc.roleID)
		}
		return w.dispatchTask(ctx, teamID, pc.roleID, pc.task, "", mode)
	case cmdTeamLead:
		team, ok := w.manager.Team(pc.teamID)
		if !ok {
			return DispatchResult{}, fmt.Errorf("unknown team %q", pc.teamID)
		}
		return w.dispatchTask(ctx, team.ID, team.DefaultLead, pc.task, "", mode)
	case cmdDecide:
		dec, err := w.Decide(ctx, input)
		if err != nil {
			return DispatchResult{}, err
		}
		switch dec.Action {
		case "reply":
			return DispatchResult{Kind: KindReply, Reply: dec.Reply}, nil
		case "delegate":
			reply := dec.Reply
			if reply == "" {
				reply = DelegateAck(dec.TeamID, dec.RoleID)
			}
			return w.dispatchTask(ctx, dec.TeamID, dec.RoleID, dec.Task, reply, mode)
		default:
			return DispatchResult{}, fmt.Errorf("unsupported action %q", dec.Action)
		}
	default:
		if pc.raw == "" {
			return DispatchResult{}, fmt.Errorf("empty input")
		}
		dec, err := w.Decide(ctx, input)
		if err != nil {
			return DispatchResult{}, err
		}
		if dec.Action == "reply" {
			return DispatchResult{Kind: KindReply, Reply: dec.Reply}, nil
		}
		reply := dec.Reply
		if reply == "" {
			reply = DelegateAck(dec.TeamID, dec.RoleID)
		}
		return w.dispatchTask(ctx, dec.TeamID, dec.RoleID, dec.Task, reply, mode)
	}
}

func (w *Workspace) dispatchTask(ctx context.Context, teamID, roleID, task, ack string, mode DispatchMode) (DispatchResult, error) {
	if err := w.manager.ValidateTarget(teamID, roleID); err != nil {
		return DispatchResult{}, err
	}
	worker, ok := w.manager.WorkerFor(teamID, roleID)
	if !ok {
		return DispatchResult{}, fmt.Errorf("worker not found")
	}

	queued := w.manager.TaskUserContent(teamID, task)
	if !w.manager.TryClaimOrPushNote(teamID, roleID, worker.Session.Agent, queued) {
		reply := ack
		if reply == "" {
			reply = fmt.Sprintf("Queued task for %s.", worker.Name)
		}
		return DispatchResult{
			Kind:   KindDelegate,
			Reply:  reply,
			TeamID: teamID,
			RoleID: roleID,
		}, nil
	}

	run := func() WorkerResult {
		runCtx, cancelRun := context.WithCancel(w.ctx)
		stopWatch := context.AfterFunc(ctx, cancelRun)
		defer func() {
			stopWatch()
			cancelRun()
		}()

		var answer string
		var err error
		userMsg := w.manager.TaskUserContent(teamID, task)
		continuation := false
		for {
			instruction := userMsg
			if continuation {
				instruction = ""
			}
			answer, err = worker.Session.Agent.Run(runCtx, worker.Session.Context, instruction)
			if err != nil {
				w.manager.Release(teamID, roleID)
				break
			}
			if w.manager.FinishWorkerRun(teamID, roleID, worker.Session.Agent) {
				break
			}
			continuation = true
		}
		if err == nil && strings.TrimSpace(answer) != "" {
			w.recordRecent(teamID+"/"+roleID, answer)
		}
		return WorkerResult{Reply: answer, Err: err}
	}

	if mode == DispatchSync {
		result := run()
		reply := ack
		if reply == "" {
			reply = result.Reply
		}
		if result.Err != nil {
			return DispatchResult{Kind: KindDelegate, Reply: reply, TeamID: teamID, RoleID: roleID}, result.Err
		}
		return DispatchResult{Kind: KindDelegate, Reply: reply, TeamID: teamID, RoleID: roleID}, nil
	}

	done := make(chan WorkerResult, 1)
	go func() {
		done <- run()
	}()
	reply := ack
	if reply == "" {
		reply = DelegateAck(teamID, roleID)
	}
	return DispatchResult{
		Kind:   KindDelegate,
		Reply:  reply,
		TeamID: teamID,
		RoleID: roleID,
		Done:   done,
	}, nil
}
