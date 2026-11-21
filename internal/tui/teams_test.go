package tui

import "testing"

func TestTeamStatuses(t *testing.T) {
	buf := NewChatBuffer()
	buf.SetTeams([]TeamLine{
		{DeptName: "Engineering", RoleID: "software_engineer", Name: "Software Engineer", State: WorkerIdle},
		{DeptName: "Engineering", RoleID: "code_review", Name: "Code Review", State: WorkerIdle},
	})
	settings := DefaultSettings()
	src := SourceRef{RoleID: "software_engineer", Name: "Software Engineer"}
	buf.ApplyLiveEvent(WorkerStateEvent{SourceRef: src, State: WorkerRunning}, settings)
	if buf.Teams[0].State != WorkerRunning {
		t.Fatal("running")
	}
	buf.ApplyLiveEvent(WorkerStateEvent{SourceRef: src, State: WorkerDone}, settings)
	if buf.Teams[0].State != WorkerDone {
		t.Fatal("done")
	}
	errSrc := SourceRef{RoleID: "code_review"}
	buf.ApplyLiveEvent(WorkerStateEvent{SourceRef: errSrc, State: WorkerError}, settings)
	if buf.Teams[1].State != WorkerError {
		t.Fatal("error")
	}
}
