package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mobius/pkg/agent"
	"mobius/pkg/agentctx"
	"mobius/pkg/llm"
	"mobius/pkg/roles"
	"mobius/pkg/tools"
)

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool, label string) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", label)
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
}

func chdirModuleRoot(t *testing.T) {
	t.Helper()
	start, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "config", "agent.toml")); err == nil {
				if err := os.Chdir(dir); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chdir(start) })
				return
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("module root not found from %s", start)
		}
		dir = parent
	}
}

type recordingProvider struct {
	mu                 sync.Mutex
	requests           []*llm.ChatRequest
	blockCh            chan struct{}
	releaseCh          chan struct{}
	call               atomic.Int32
	replies            []string
	afterWorkerGen     chan struct{}
	blockFromWorkerGen int
	holdWorkerGen      chan struct{}
	workerGenCount     atomic.Int32
}

func (p *recordingProvider) Name() string { return "recording" }

func (p *recordingProvider) Generate(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	p.mu.Lock()
	cp := &llm.ChatRequest{
		Model:   req.Model,
		Message: append([]llm.Message(nil), req.Message...),
		Tools:   append([]llm.ToolDefinition(nil), req.Tools...),
	}
	p.requests = append(p.requests, cp)
	n := int(p.call.Add(1))
	block := p.blockCh
	release := p.releaseCh
	replies := append([]string(nil), p.replies...)
	p.mu.Unlock()

	if block != nil && len(req.Tools) == 0 && n == 1 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
		}
	}
	if len(req.Tools) > 0 {
		wn := int(p.workerGenCount.Add(1))
		p.mu.Lock()
		hold := p.holdWorkerGen
		p.mu.Unlock()
		if hold != nil && wn == 1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-hold:
			}
		}
		if block != nil && (p.blockFromWorkerGen == 0 || wn >= p.blockFromWorkerGen) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
			}
		}
		p.mu.Lock()
		ch := p.afterWorkerGen
		p.mu.Unlock()
		if ch != nil {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
		return &llm.ChatResponse{Content: "worker-finished", Usage: llm.Usage{PromptTokens: 1, CompletionTokens: 1}}, nil
	}
	var content string
	if n-1 < len(replies) {
		content = replies[n-1]
	} else {
		content = `{"think":"ok","action":"reply","reply":"hello","team_id":"","role_id":"","task":""}`
	}
	return &llm.ChatResponse{Content: content, Usage: llm.Usage{PromptTokens: 1, CompletionTokens: 1}}, nil
}

func (p *recordingProvider) lastRequest() *llm.ChatRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) == 0 {
		return nil
	}
	return p.requests[len(p.requests)-1]
}

func (p *recordingProvider) orchestratorCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	count := 0
	for _, r := range p.requests {
		if len(r.Tools) == 0 && len(r.Message) >= 2 {
			count++
		}
	}
	return count
}

func testFactory(provider llm.Provider) AgentFactory {
	return func(registry *tools.Registry, model ModelSpec) (*agent.Agent, error) {
		m := model
		m.Provider = provider
		return agent.NewAgent(provider, registry, m.Name, m.PromptPrice, m.CompletionPrice, nil)
	}
}

func newTestWorkspace(t *testing.T, provider *recordingProvider) *Workspace {
	t.Helper()
	store, err := roles.Open(context.Background(), "roles")
	if err != nil {
		t.Fatalf("roles.Open: %v", err)
	}
	base := tools.NewDefaultRegistry(".")
	model := ModelSpec{Name: "test-model", Provider: provider, PromptPrice: 1, CompletionPrice: 1}
	ws, err := NewWorkspace(context.Background(), "chat-1", store, base, model, testFactory(provider))
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	t.Cleanup(ws.Close)
	return ws
}

const secretSentinel = "SECRET_USER_SENTINEL_XYZ"

func TestDirectReplyNoWorker(t *testing.T) {
	chdirModuleRoot(t)
	provider := &recordingProvider{
		replies: []string{
			`{"think":"t","action":"reply","reply":"direct","team_id":"","role_id":"","task":""}`,
		},
	}
	ws := newTestWorkspace(t, provider)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := ws.Handle(ctx, secretSentinel, DispatchSync)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Kind != KindReply || res.Reply != "direct" {
		t.Fatalf("unexpected result: %+v", res)
	}
	msgs := ws.OrchestratorSession().Context.Messages()
	if len(msgs) < 3 || msgs[len(msgs)-1].Content != "direct" {
		t.Fatalf("conversation not recorded: %+v", msgs)
	}
	if !strings.Contains(ws.OrchestratorSession().Agent.BudgetStatus(), "1 In") {
		t.Fatalf("cost not recorded: %s", ws.OrchestratorSession().Agent.BudgetStatus())
	}
	if res.Done != nil {
		t.Fatal("expected nil Done for direct reply")
	}
	req := provider.lastRequest()
	if req == nil {
		t.Fatal("no provider request")
	}
	if len(req.Tools) != 0 {
		t.Fatalf("orchestrator request must have zero tools, got %d", len(req.Tools))
	}
	for _, m := range req.Message {
		if m.Role == llm.RoleSystem && strings.Contains(m.Content, secretSentinel) {
			t.Fatal("secret sentinel leaked into system message")
		}
	}
	if provider.orchestratorCalls() != 1 {
		t.Fatalf("expected one orchestrator call, got %d", provider.orchestratorCalls())
	}
}

func TestDelegateAsyncReturnsBeforeWorker(t *testing.T) {
	chdirModuleRoot(t)
	provider := &recordingProvider{
		blockCh:   make(chan struct{}),
		releaseCh: make(chan struct{}),
		replies: []string{
			`{"think":"t","action":"delegate","reply":"","team_id":"engineering","role_id":"software_engineer","task":"do work"}`,
		},
	}
	ws := newTestWorkspace(t, provider)
	ctx := context.Background()

	res, err := ws.Handle(ctx, "*Software Engineer do work", DispatchAsync)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Kind != KindDelegate {
		t.Fatalf("expected delegate, got %v", res.Kind)
	}
	if res.Done == nil {
		t.Fatal("expected Done channel")
	}
	select {
	case <-res.Done:
		t.Fatal("async Handle should not wait for worker")
	case <-time.After(30 * time.Millisecond):
	}
	worker, _ := ws.manager.WorkerFor("engineering", "software_engineer")
	if worker.Session.Name() != "Software Engineer" {
		t.Fatalf("worker name changed: %q", worker.Session.Name())
	}
	close(provider.releaseCh)
	select {
	case wr := <-res.Done:
		if wr.Err != nil {
			t.Fatalf("worker error: %v", wr.Err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not finish")
	}
}

func TestMentionBypassesOrchestrator(t *testing.T) {
	chdirModuleRoot(t)
	provider := &recordingProvider{blockCh: make(chan struct{}), releaseCh: make(chan struct{})}
	ws := newTestWorkspace(t, provider)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := ws.Handle(ctx, "*Software Engineer fix the bug", DispatchAsync)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Kind != KindDelegate {
		t.Fatalf("expected delegate, got %v", res.Kind)
	}
	if provider.orchestratorCalls() != 0 {
		t.Fatal("orchestrator model must not be called for *mention delegate test")
	}
	close(provider.releaseCh)
	<-res.Done
}

func (p *recordingProvider) workerGenerateCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, r := range p.requests {
		if len(r.Tools) > 0 {
			n++
		}
	}
	return n
}

func (p *recordingProvider) workerRequest(n int) *llm.ChatRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	idx := 0
	for _, r := range p.requests {
		if len(r.Tools) == 0 {
			continue
		}
		idx++
		if idx == n {
			cp := *r
			cp.Message = append([]llm.Message(nil), r.Message...)
			return &cp
		}
	}
	return nil
}

const lateNoteSentinel = "LATE_NOTE_SENTINEL_BEFORE_RELEASE"

func TestLatePushNoteKeepsClaimUntilContinuationRun(t *testing.T) {
	chdirModuleRoot(t)
	hold := make(chan struct{})
	provider := &recordingProvider{
		blockCh:            make(chan struct{}),
		releaseCh:          make(chan struct{}),
		holdWorkerGen:      hold,
		blockFromWorkerGen: 2,
	}
	ws := newTestWorkspace(t, provider)
	ctx := context.Background()
	worker, _ := ws.manager.WorkerFor("engineering", "software_engineer")

	res, err := ws.Handle(ctx, "*Software Engineer first task", DispatchAsync)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	waitUntil(t, 3*time.Second, func() bool { return provider.workerGenerateCount() >= 1 }, "first worker Generate")

	if !worker.Session.Agent.PushNote(lateNoteSentinel) {
		t.Fatal("PushNote rejected")
	}
	close(hold)

	waitUntil(t, 3*time.Second, func() bool { return provider.workerGenerateCount() >= 2 }, "continuation worker Generate")

	if !ws.manager.TryClaim("engineering", "software_engineer") {
		// expected: still busy
	} else {
		t.Fatal("TryClaim must not succeed before continuation finishes")
		ws.manager.Release("engineering", "software_engineer")
	}

	req2 := provider.workerRequest(2)
	if req2 == nil {
		t.Fatal("missing second worker request")
	}
	found := false
	for _, m := range req2.Message {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, lateNoteSentinel) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("second Generate must include sentinel; messages: %+v", req2.Message)
	}

	close(provider.releaseCh)
	select {
	case <-res.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish")
	}

	if !ws.manager.TryClaim("engineering", "software_engineer") {
		t.Fatal("TryClaim should succeed after worker released")
	}
	ws.manager.Release("engineering", "software_engineer")
}

func TestClaimConflictQueuesNote(t *testing.T) {
	chdirModuleRoot(t)
	provider := &recordingProvider{
		blockCh:   make(chan struct{}),
		releaseCh: make(chan struct{}),
	}
	ws := newTestWorkspace(t, provider)
	ctx := context.Background()

	res1, err := ws.Handle(ctx, "*Software Engineer first task", DispatchAsync)
	if err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	deadline := time.After(2 * time.Second)
	for !ws.manager.IsClaimed("engineering", "software_engineer") {
		select {
		case <-deadline:
			t.Fatal("worker not claimed")
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}

	res2, err := ws.Handle(ctx, "*Software Engineer second task", DispatchAsync)
	if err != nil {
		t.Fatalf("second Handle: %v", err)
	}
	if res2.Kind != KindDelegate {
		t.Fatalf("expected delegate queue, got %v", res2.Kind)
	}

	waitUntil(t, 3*time.Second, func() bool {
		provider.mu.Lock()
		defer provider.mu.Unlock()
		for _, r := range provider.requests {
			if len(r.Tools) > 0 {
				return true
			}
		}
		return false
	}, "worker Generate in flight")
	worker, _ := ws.manager.WorkerFor("engineering", "software_engineer")

	close(provider.releaseCh)
	<-res1.Done

	msgs := worker.Session.Context.Messages()
	var queued bool
	for _, m := range msgs {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "second task") {
			queued = true
		}
	}
	if !queued {
		t.Fatal("second task should arrive as a later user note")
	}
	_ = res2
}

func TestSteerOnlyRunningWorker(t *testing.T) {
	chdirModuleRoot(t)
	provider := &recordingProvider{
		blockCh:   make(chan struct{}),
		releaseCh: make(chan struct{}),
	}
	ws := newTestWorkspace(t, provider)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := ws.Handle(ctx, "*Software Engineer long task", DispatchAsync)
	if err != nil {
		t.Fatalf("start worker: %v", err)
	}

	for !ws.manager.IsClaimed("engineering", "software_engineer") {
		time.Sleep(2 * time.Millisecond)
	}

	_, err = ws.Handle(ctx, "steer engineering focus on tests", DispatchSync)
	if err != nil {
		t.Fatalf("steer: %v", err)
	}

	se, _ := ws.manager.WorkerFor("engineering", "software_engineer")
	cr, _ := ws.manager.WorkerFor("engineering", "code_review")

	for _, m := range cr.Session.Context.Messages() {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "focus on tests") {
			t.Fatal("idle Code Review must not receive immediate steer note")
		}
	}
	if !directiveStored(ws.manager, "engineering", "focus on tests") {
		t.Fatal("directive not stored on team")
	}

	close(provider.releaseCh)
	<-res.Done
	_ = se
	_ = cr
}

func TestTwoWorkspacesDistinctAgents(t *testing.T) {
	chdirModuleRoot(t)
	store, err := roles.Open(context.Background(), "roles")
	if err != nil {
		t.Fatal(err)
	}
	p1 := &recordingProvider{releaseCh: make(chan struct{})}
	p2 := &recordingProvider{releaseCh: make(chan struct{})}
	base := tools.NewDefaultRegistry(".")
	model := ModelSpec{Name: "m", PromptPrice: 1, CompletionPrice: 1}

	ws1, err := NewWorkspace(context.Background(), "c1", store, base, model, testFactory(p1))
	if err != nil {
		t.Fatal(err)
	}
	defer ws1.Close()
	ws2, err := NewWorkspace(context.Background(), "c2", store, base, model, testFactory(p2))
	if err != nil {
		t.Fatal(err)
	}
	defer ws2.Close()

	w1, _ := ws1.manager.WorkerFor("engineering", "software_engineer")
	w2, _ := ws2.manager.WorkerFor("engineering", "software_engineer")
	if w1.Session.Agent.ThreadID() == w2.Session.Agent.ThreadID() {
		t.Fatal("workspaces must not share worker agents")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r1, err := ws1.Handle(ctx, "*Software Engineer task_a", DispatchAsync)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := ws2.Handle(ctx, "*Software Engineer task_b", DispatchAsync)
	if err != nil {
		t.Fatal(err)
	}
	close(p1.releaseCh)
	close(p2.releaseCh)
	<-r1.Done
	<-r2.Done
}

func TestSteerNoteInConversation(t *testing.T) {
	chdirModuleRoot(t)
	provider := &recordingProvider{blockCh: make(chan struct{}), releaseCh: make(chan struct{})}
	ws := newTestWorkspace(t, provider)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, _ := ws.Handle(ctx, "*Software Engineer work", DispatchAsync)
	for !ws.manager.IsClaimed("engineering", "software_engineer") {
		time.Sleep(2 * time.Millisecond)
	}
	_, _ = ws.Handle(ctx, "steer engineering focus on tests", DispatchSync)
	se, _ := ws.manager.WorkerFor("engineering", "software_engineer")
	close(provider.releaseCh)
	<-res.Done

	found := false
	for _, m := range se.Session.Context.Messages() {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "focus on tests") {
			found = true
		}
	}
	if !found {
		t.Fatal("steer should reach running worker conversation")
	}
}

func directiveStored(mgr *Manager, teamID, text string) bool {
	team, ok := mgr.Team(teamID)
	if !ok {
		return false
	}
	for _, d := range team.directives {
		if strings.Contains(d, text) {
			return true
		}
	}
	return false
}

func TestDecideUserMessageJSON(t *testing.T) {
	chdirModuleRoot(t)
	provider := &recordingProvider{
		replies: []string{`{"think":"t","action":"reply","reply":"ok","team_id":"","role_id":"","task":""}`},
	}
	ws := newTestWorkspace(t, provider)
	_, err := ws.Handle(context.Background(), "hello \"world\"", DispatchSync)
	if err != nil {
		t.Fatal(err)
	}
	req := provider.lastRequest()
	var userBody string
	for _, m := range req.Message {
		if m.Role == llm.RoleUser {
			userBody = m.Content
		}
	}
	if !strings.Contains(userBody, "<user_message>") {
		t.Fatal("expected user_message tag")
	}
	var inner string
	start := strings.Index(userBody, "<user_message>")
	end := strings.Index(userBody, "</user_message>")
	inner = strings.TrimSpace(userBody[start+len("<user_message>") : end])
	var decoded string
	if err := json.Unmarshal([]byte(inner), &decoded); err != nil {
		t.Fatalf("user_message must be JSON string: %v", err)
	}
}

func TestCatalogSnapshotRoundTrip(t *testing.T) {
	chdirModuleRoot(t)
	ws := newTestWorkspace(t, &recordingProvider{})
	snap := ws.manager.CatalogSnapshot()
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"engineering",
		"software_engineer",
		"code_review",
		"product",
		"product_manager",
		"Read-only code review",
		"Clarify user problems",
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("catalog missing %q: %s", want, data)
		}
	}
}

func TestAgentctxIsolation(t *testing.T) {
	ctx := agentctx.NewConversationContext("sys")
	ctx.AddUserMessage("one")
	msgs := ctx.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestWorkerPromptLayersBaseAndRoleWhileOrchestratorStaysRouterOnly(t *testing.T) {
	chdirModuleRoot(t)
	ws := newTestWorkspace(t, &recordingProvider{})

	orchSys := ws.OrchestratorSession().Context.Messages()[0].Content
	if !strings.Contains(orchSys, "<routing_rules>") {
		t.Fatalf("orchestrator system prompt missing routing rules: %s", orchSys)
	}
	if strings.Contains(orchSys, "<tools>") {
		t.Fatalf("orchestrator prompt should not include worker tool rules: %s", orchSys)
	}

	se, ok := ws.manager.WorkerFor("engineering", "software_engineer")
	if !ok {
		t.Fatal("missing software_engineer worker")
	}
	seSys := se.Session.Context.Messages()[0].Content
	for _, want := range []string{
		"<identity>",
		"<grounding>",
		"<tools>",
		"<role_instructions>",
		"<role_persona>",
		"Software Engineer",
	} {
		if !strings.Contains(seSys, want) {
			t.Fatalf("worker prompt missing %q:\n%s", want, seSys)
		}
	}

	snap := ws.manager.CatalogSnapshot()
	var foundReview bool
	for _, team := range snap.Teams {
		for _, r := range team.Roles {
			if r.ID == "code_review" {
				foundReview = true
				if len(r.Tools) == 0 {
					t.Fatal("expected catalog role tools to be populated")
				}
				r.Tools[0] = "mutated"
			}
		}
	}
	if !foundReview {
		t.Fatal("missing code_review in catalog snapshot")
	}
	snap2 := ws.manager.CatalogSnapshot()
	for _, team := range snap2.Teams {
		for _, r := range team.Roles {
			if r.ID == "code_review" && r.Tools[0] == "mutated" {
				t.Fatal("mutating CatalogSnapshot tools leaked into manager state")
			}
		}
	}
}

func TestReloadRolesPreservesUnchangedAndRebuildsChanged(t *testing.T) {
	chdirModuleRoot(t)
	dir := t.TempDir()
	writeRoleFixture := func(sys, toolsYAML string) {
		if err := os.MkdirAll(filepath.Join(dir, "departments"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "software_engineer.yml"), []byte(
			"id: software_engineer\nname: Software Engineer\ndepartment: engineering\nsystem: |\n  "+sys+"\ntools:\n"+toolsYAML,
		), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "departments", "engineering.yml"), []byte(
			"id: engineering\nname: Engineering\nroles:\n  - software_engineer\ndefault_lead: software_engineer\n",
		), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writeRoleFixture("Version one.", "  - view_file\n")
	store, err := roles.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingProvider{}
	base := tools.NewDefaultRegistry(".")
	model := ModelSpec{Name: "test-model", Provider: provider, PromptPrice: 1, CompletionPrice: 1}
	ws, err := NewWorkspace(context.Background(), "chat-reload", store, base, model, testFactory(provider))
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	before, _ := ws.manager.WorkerFor("engineering", "software_engineer")
	beforeThread := before.Session.Agent.ThreadID()

	if err := ws.manager.ReloadRoles(context.Background()); err != nil {
		t.Fatalf("unchanged ReloadRoles: %v", err)
	}
	same, _ := ws.manager.WorkerFor("engineering", "software_engineer")
	if same.Session.Agent.ThreadID() != beforeThread {
		t.Fatal("unchanged role should keep its existing session")
	}

	writeRoleFixture("Version two.", "  - view_file\n  - grep_search\n")
	if err := ws.manager.ReloadRoles(context.Background()); err != nil {
		t.Fatalf("changed ReloadRoles: %v", err)
	}
	after, _ := ws.manager.WorkerFor("engineering", "software_engineer")
	if after.Session.Agent.ThreadID() == beforeThread {
		t.Fatal("changed role should rebuild worker session")
	}
	afterSys := after.Session.Context.Messages()[0].Content
	if !strings.Contains(afterSys, "Version two.") || !strings.Contains(afterSys, "<identity>") {
		t.Fatalf("rebuilt worker missing layered prompt:\n%s", afterSys)
	}
}

func TestDecideIncludesBoundedRedactedRecentTurnsOnFollowUp(t *testing.T) {
	chdirModuleRoot(t)
	provider := &recordingProvider{
		replies: []string{
			`{"think":"review","action":"delegate","reply":"","team_id":"engineering","role_id":"code_review","task":"Review pkg/roles"}`,
			"",
			`{"think":"follow-up","action":"reply","reply":"done","team_id":"","role_id":"","task":""}`,
		},
	}
	ws := newTestWorkspace(t, provider)
	ctx := context.Background()

	// Turn 1: should have no <recent_turns> yet.
	if _, err := ws.Handle(ctx, "review pkg/roles with token sk-secret1234567890", DispatchSync); err != nil {
		t.Fatalf("turn 1 Handle: %v", err)
	}
	provider.mu.Lock()
	var turn1Req *llm.ChatRequest
	for _, r := range provider.requests {
		if len(r.Tools) == 0 {
			turn1Req = r
			break
		}
	}
	provider.mu.Unlock()
	if turn1Req == nil {
		t.Fatal("missing turn 1 orchestrator request")
	}
	if strings.Contains(turn1Req.Message[1].Content, "<recent_turns>") {
		t.Fatalf("turn 1 should omit empty <recent_turns>: %s", turn1Req.Message[1].Content)
	}

	// Turn 2: follow-up should include <recent_turns> with redacted prior turn and worker summary.
	res2, err := ws.Handle(ctx, "now fix the second issue", DispatchSync)
	if err != nil {
		t.Fatalf("turn 2 Handle: %v", err)
	}
	if res2.Reply != "done" {
		t.Fatalf("unexpected turn 2 reply: %q", res2.Reply)
	}
	turn2Req := provider.lastRequest()
	if turn2Req == nil || len(turn2Req.Tools) != 0 {
		t.Fatalf("expected turn 2 orchestrator request, got %+v", turn2Req)
	}
	if strings.Contains(turn2Req.Message[0].Content, "review pkg/roles") {
		t.Fatal("recent turns must never leak into system message")
	}
	userBody := turn2Req.Message[1].Content
	if !strings.Contains(userBody, "<recent_turns>") {
		t.Fatalf("turn 2 missing <recent_turns>: %s", userBody)
	}
	if strings.Contains(userBody, "sk-secret1234567890") {
		t.Fatalf("secret leaked into <recent_turns>: %s", userBody)
	}
	if !strings.Contains(userBody, "[REDACTED]") {
		t.Fatalf("expected redacted placeholder in <recent_turns>: %s", userBody)
	}
	if !strings.Contains(userBody, "engineering/code_review") || !strings.Contains(userBody, "worker-finished") {
		t.Fatalf("expected completed worker outcome in <recent_turns>: %s", userBody)
	}

	// Verify bounding on count and rune length under concurrent writes.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				ws.recordRecent("user", strings.Repeat("a", maxRecentRunes+100))
				_ = ws.recentSnapshot()
			}
		}()
	}
	wg.Wait()

	snap := ws.recentSnapshot()
	if len(snap) > maxRecentTurns {
		t.Fatalf("recent turns exceeded cap: %d > %d", len(snap), maxRecentTurns)
	}
	for _, entry := range snap {
		if len([]rune(entry.Content)) > maxRecentRunes+1 {
			t.Fatalf("recent entry exceeded rune cap: %d", len([]rune(entry.Content)))
		}
	}
}
