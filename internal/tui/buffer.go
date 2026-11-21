package tui

import (
	"fmt"
	"strings"
	"sync"

	"mobius/internal/tui/markdown"
)

// BlockKind classifies transcript blocks.
type BlockKind int

const (
	BlockUser BlockKind = iota
	BlockFinal
	BlockStreaming
	BlockThought
	BlockStep
	BlockToolGroup
	BlockSensor
	BlockTrace
	BlockError
)

// TranscriptBlock is one rendered unit in the chat log.
type TranscriptBlock struct {
	ID            string
	Kind          BlockKind
	Speaker       string
	RoleID        string
	RunID         string
	Text          string
	StreamingTool string
	Tools         []AgentToolEvent
	Expanded      bool
	ToolCount     int
	ErrorKey      string
}

// ChatBuffer owns one chat session's transcript and UI-side state.
type ChatBuffer struct {
	mu             sync.Mutex
	Blocks         []TranscriptBlock
	Draft          string
	ViewFilter     string // "" = All
	ScrollOff      int
	AutoFollow     bool
	Unread         int
	RunCounter     int
	Teams          []TeamLine
	activeRun      string
	pendingThought int // index into Blocks, -1
	lastToolFold   int // index of open tool group
	seenErrors     map[string]struct{}
	ToolsExpanded  bool
}

type TeamLine struct {
	DeptName string
	RoleID   string
	Name     string
	State    WorkerState
}

func NewChatBuffer() *ChatBuffer {
	return &ChatBuffer{
		AutoFollow:     true,
		pendingThought: -1,
		lastToolFold:   -1,
		seenErrors:     make(map[string]struct{}),
	}
}

// ChatStore maps session ids to buffers.
type ChatStore struct {
	mu       sync.Mutex
	activeID string
	chats    map[string]*ChatBuffer
}

func NewChatStore(activeID string) *ChatStore {
	cs := &ChatStore{chats: make(map[string]*ChatBuffer)}
	cs.Ensure(activeID)
	cs.activeID = activeID
	return cs
}

func (s *ChatStore) Ensure(id string) *ChatBuffer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureLocked(id)
}

func (s *ChatStore) ensureLocked(id string) *ChatBuffer {
	if b, ok := s.chats[id]; ok {
		return b
	}
	b := NewChatBuffer()
	s.chats[id] = b
	return b
}

func (s *ChatStore) Active() *ChatBuffer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chats[s.activeID]
}

func (s *ChatStore) SwitchActive(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked(id)
	s.activeID = id
}

func (s *ChatStore) Buffer(id string) *ChatBuffer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureLocked(id)
}

func (b *ChatBuffer) BeginRun() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.RunCounter++
	b.activeRun = fmt.Sprintf("run-%d", b.RunCounter)
	b.pendingThought = -1
	return b.activeRun
}

func (b *ChatBuffer) AppendUser(text, runID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Blocks = append(b.Blocks, TranscriptBlock{
		ID:      fmt.Sprintf("u-%d", len(b.Blocks)),
		Kind:    BlockUser,
		Speaker: "user",
		RunID:   runID,
		Text:    text,
	})
	b.bumpFollow()
}

func (b *ChatBuffer) bumpFollow() {
	if b.AutoFollow {
		b.ScrollOff = 0
	} else {
		b.Unread++
	}
}

// ApplyLiveEvent updates buffer state; budget events update only the ledger outside.
func (b *ChatBuffer) ApplyLiveEvent(ev LiveEvent, settings Settings) {
	switch e := ev.(type) {
	case StreamDeltaEvent:
		b.mu.Lock()
		key := streamKey(e.SourceRef.RoleID, e.SourceRef.Name)
		if e.TextDelta != "" {
			chunk := markdown.StripANSI(e.TextDelta)
			if chunk != "" {
				if idx := b.findStreaming(key); idx >= 0 {
					b.Blocks[idx].StreamingTool = ""
					b.Blocks[idx].Text += chunk
				} else {
					b.Blocks = append(b.Blocks, TranscriptBlock{
						ID:      fmt.Sprintf("st-%d", len(b.Blocks)),
						Kind:    BlockStreaming,
						Speaker: e.SourceRef.Name,
						RoleID:  e.SourceRef.RoleID,
						RunID:   pickRun(e.RunID, b.activeRun),
						Text:    chunk,
					})
				}
				b.bumpFollow()
			}
		}
		if e.ToolName != "" {
			tool := markdown.StripANSI(e.ToolName)
			if idx := b.findStreaming(key); idx >= 0 {
				b.Blocks[idx].Text = ""
				b.Blocks[idx].StreamingTool = tool
			} else {
				b.Blocks = append(b.Blocks, TranscriptBlock{
					ID:            fmt.Sprintf("st-%d", len(b.Blocks)),
					Kind:          BlockStreaming,
					Speaker:       e.SourceRef.Name,
					RoleID:        e.SourceRef.RoleID,
					RunID:         pickRun(e.RunID, b.activeRun),
					StreamingTool: tool,
				})
			}
			b.bumpFollow()
		}
		b.mu.Unlock()
	case AgentStepEvent:
		if !settings.ShowSteps {
			return
		}
		b.mu.Lock()
		b.clearPendingThought()
		b.removeStreaming(streamKey(e.SourceRef.RoleID, e.SourceRef.Name))
		b.Blocks = append(b.Blocks, TranscriptBlock{
			ID:      fmt.Sprintf("s-%d", len(b.Blocks)),
			Kind:    BlockStep,
			Speaker: e.SourceRef.Name,
			RoleID:  e.SourceRef.RoleID,
			RunID:   b.activeRun,
			Text:    e.Text,
		})
		b.bumpFollow()
		b.mu.Unlock()
	case AgentThoughtEvent:
		if !settings.ShowSteps {
			return
		}
		b.mu.Lock()
		idx := len(b.Blocks)
		b.Blocks = append(b.Blocks, TranscriptBlock{
			ID:      fmt.Sprintf("t-%d", idx),
			Kind:    BlockThought,
			Speaker: e.SourceRef.Name,
			RoleID:  e.SourceRef.RoleID,
			RunID:   pickRun(e.RunID, b.activeRun),
			Text:    e.Text,
		})
		b.pendingThought = idx
		b.bumpFollow()
		b.mu.Unlock()
	case AgentToolEvent:
		if !settings.ShowSteps {
			b.mu.Lock()
			b.removeStreaming(streamKey(e.SourceRef.RoleID, e.SourceRef.Name))
			b.mu.Unlock()
			return
		}
		b.mu.Lock()
		b.clearPendingThought()
		b.removeStreaming(streamKey(e.SourceRef.RoleID, e.SourceRef.Name))
		b.foldTool(e)
		b.bumpFollow()
		b.mu.Unlock()
	case AgentSensorEvent:
		if !settings.ShowSteps {
			b.mu.Lock()
			b.removeStreaming(streamKey(e.SourceRef.RoleID, e.SourceRef.Name))
			b.mu.Unlock()
			return
		}
		b.mu.Lock()
		b.clearPendingThought()
		b.removeStreaming(streamKey(e.SourceRef.RoleID, e.SourceRef.Name))
		b.Blocks = append(b.Blocks, TranscriptBlock{
			ID:      fmt.Sprintf("sn-%d", len(b.Blocks)),
			Kind:    BlockSensor,
			Speaker: e.SourceRef.Name,
			RoleID:  e.SourceRef.RoleID,
			Text:    strings.TrimSpace(e.Text),
		})
		b.bumpFollow()
		b.mu.Unlock()
	case RouteTraceEvent:
		if !settings.ShowTrace {
			return
		}
		speaker := e.SourceRef.Name
		if speaker == "" {
			speaker = "orchestrator"
		}
		b.mu.Lock()
		b.Blocks = append(b.Blocks, TranscriptBlock{
			ID:      fmt.Sprintf("tr-%d", len(b.Blocks)),
			Kind:    BlockTrace,
			Speaker: speaker,
			RoleID:  e.SourceRef.RoleID,
			Text:    e.Text,
		})
		b.bumpFollow()
		b.mu.Unlock()
	case FinalReplyEvent:
		b.ResolveFinal(e.SourceRef.Name, e.SourceRef.RoleID, e.Text, pickRun(e.RunID, b.activeRun), e.Err)
	case RunErrorEvent:
		b.mu.Lock()
		b.removeStreaming(streamKey(e.SourceRef.RoleID, e.SourceRef.Name))
		key := e.Key
		if key == "" {
			key = e.Text
		}
		if _, dup := b.seenErrors[key]; dup {
			b.mu.Unlock()
			return
		}
		b.seenErrors[key] = struct{}{}
		b.Blocks = append(b.Blocks, TranscriptBlock{
			ID:       fmt.Sprintf("err-%d", len(b.Blocks)),
			Kind:     BlockError,
			Speaker:  e.SourceRef.Name,
			RoleID:   e.SourceRef.RoleID,
			Text:     e.Text,
			ErrorKey: key,
		})
		b.bumpFollow()
		b.mu.Unlock()
	case WorkerStateEvent:
		b.setRoleState(e.SourceRef.RoleID, e.State)
	}
}

func pickRun(run, active string) string {
	if run != "" {
		return run
	}
	return active
}

func (b *ChatBuffer) clearPendingThought() {
	idx := b.pendingThought
	b.pendingThought = -1
	if idx < 0 || idx >= len(b.Blocks) || b.Blocks[idx].Kind != BlockThought {
		return
	}
	b.removeBlockAt(idx)
}

func streamKey(roleID, speaker string) string {
	if roleID != "" {
		return "role:" + roleID
	}
	return "speaker:" + speaker
}

func (b *ChatBuffer) findStreaming(key string) int {
	for i := len(b.Blocks) - 1; i >= 0; i-- {
		bl := b.Blocks[i]
		if bl.Kind == BlockStreaming && streamKey(bl.RoleID, bl.Speaker) == key {
			return i
		}
	}
	return -1
}

func (b *ChatBuffer) removeStreaming(key string) {
	if idx := b.findStreaming(key); idx >= 0 {
		b.removeBlockAt(idx)
	}
}

func (b *ChatBuffer) removeBlockAt(idx int) {
	if idx < 0 || idx >= len(b.Blocks) {
		return
	}
	b.Blocks = append(b.Blocks[:idx], b.Blocks[idx+1:]...)
	if b.pendingThought > idx {
		b.pendingThought--
	} else if b.pendingThought == idx {
		b.pendingThought = -1
	}
	if b.lastToolFold > idx {
		b.lastToolFold--
	} else if b.lastToolFold == idx {
		b.lastToolFold = -1
	}
}

func (b *ChatBuffer) foldTool(e AgentToolEvent) {
	ev := e
	if b.lastToolFold >= 0 && b.lastToolFold < len(b.Blocks) {
		g := &b.Blocks[b.lastToolFold]
		if g.Kind == BlockToolGroup && g.RoleID == e.SourceRef.RoleID {
			g.Tools = append(g.Tools, ev)
			g.ToolCount = len(g.Tools)
			g.Expanded = b.ToolsExpanded
			return
		}
	}
	idx := len(b.Blocks)
	b.Blocks = append(b.Blocks, TranscriptBlock{
		ID:        fmt.Sprintf("tg-%d", idx),
		Kind:      BlockToolGroup,
		Speaker:   e.SourceRef.Name,
		RoleID:    e.SourceRef.RoleID,
		Tools:     []AgentToolEvent{ev},
		ToolCount: 1,
		Expanded:  b.ToolsExpanded,
	})
	b.lastToolFold = idx
}

// ResolveFinal promotes the last streaming or pending thought block, or appends a final reply block.
func (b *ChatBuffer) ResolveFinal(speaker, roleID, text, runID string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sKey := streamKey(roleID, speaker)
	if err != nil {
		b.removeStreaming(sKey)
		key := speaker + ":" + err.Error()
		if _, dup := b.seenErrors[key]; !dup {
			b.seenErrors[key] = struct{}{}
			b.Blocks = append(b.Blocks, TranscriptBlock{
				Kind:     BlockError,
				Speaker:  speaker,
				RoleID:   roleID,
				Text:     err.Error(),
				ErrorKey: key,
			})
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		b.removeStreaming(sKey)
		return
	}
	if idx := b.findStreaming(sKey); idx >= 0 {
		st := &b.Blocks[idx]
		st.Kind = BlockFinal
		st.Speaker = speaker
		st.RoleID = roleID
		st.RunID = runID
		st.Text = text
		st.StreamingTool = ""
		b.clearPendingThought()
		b.bumpFollow()
		return
	}
	if b.pendingThought >= 0 && b.pendingThought < len(b.Blocks) {
		th := &b.Blocks[b.pendingThought]
		if th.Kind == BlockThought && th.Text == text {
			th.Kind = BlockFinal
			th.Speaker = speaker
			th.RoleID = roleID
			th.RunID = runID
			b.pendingThought = -1
			b.bumpFollow()
			return
		}
	}
	b.Blocks = append(b.Blocks, TranscriptBlock{
		Kind:    BlockFinal,
		Speaker: speaker,
		RoleID:  roleID,
		RunID:   runID,
		Text:    text,
	})
	b.pendingThought = -1
	b.bumpFollow()
}

func (b *ChatBuffer) setRoleState(roleID string, state WorkerState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.Teams {
		if b.Teams[i].RoleID == roleID {
			b.Teams[i].State = state
		}
	}
}

func (b *ChatBuffer) SetTeams(lines []TeamLine) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Teams = append([]TeamLine(nil), lines...)
}

func (b *ChatBuffer) ToggleToolExpand() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ToolsExpanded = !b.ToolsExpanded
	for i := range b.Blocks {
		if b.Blocks[i].Kind == BlockToolGroup {
			b.Blocks[i].Expanded = b.ToolsExpanded
		}
	}
}

// VisibleBlocks returns blocks after view filter.
func (b *ChatBuffer) VisibleBlocks() []TranscriptBlock {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ViewFilter == "" {
		out := make([]TranscriptBlock, len(b.Blocks))
		copy(out, b.Blocks)
		return out
	}
	var out []TranscriptBlock
	for _, bl := range b.Blocks {
		if bl.Kind == BlockUser {
			out = append(out, bl)
			continue
		}
		if bl.RoleID == b.ViewFilter || bl.Speaker == roleNameForFilter(b.ViewFilter) {
			out = append(out, bl)
		}
	}
	return out
}

func roleNameForFilter(roleID string) string {
	return roleID
}

// HasBudgetInTranscript is a test helper — budget must never appear.
func (b *ChatBuffer) HasBudgetInTranscript() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, bl := range b.Blocks {
		if strings.Contains(bl.Text, "[Budget]") {
			return true
		}
	}
	return false
}
