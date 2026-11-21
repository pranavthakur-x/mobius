package tui

import "sync"

// BudgetSnapshot is cumulative per thread from the latest [Budget] line.
type BudgetSnapshot struct {
	InTokens  int
	OutTokens int
	CostUSD   float64
	CostINR   float64
}

// BudgetLedger replaces the latest snapshot per thread; Budget lines never enter the transcript.
type BudgetLedger struct {
	mu    sync.Mutex
	byTid map[string]BudgetSnapshot
}

func NewBudgetLedger() *BudgetLedger {
	return &BudgetLedger{byTid: make(map[string]BudgetSnapshot)}
}

func (l *BudgetLedger) Apply(ev BudgetEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.byTid[ev.SourceRef.ThreadID] = BudgetSnapshot{
		InTokens:  ev.InTokens,
		OutTokens: ev.OutTokens,
		CostUSD:   ev.CostUSD,
		CostINR:   ev.CostINR,
	}
}

func (l *BudgetLedger) Total() BudgetSnapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	var t BudgetSnapshot
	for _, s := range l.byTid {
		t.InTokens += s.InTokens
		t.OutTokens += s.OutTokens
		t.CostUSD += s.CostUSD
		t.CostINR += s.CostINR
	}
	return t
}

func (l *BudgetLedger) ReplaceThread(threadID string, snap BudgetSnapshot) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.byTid[threadID] = snap
}
