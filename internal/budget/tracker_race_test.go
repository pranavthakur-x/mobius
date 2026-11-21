package budget

import (
	"mobius/pkg/llm"
	"sync"
	"testing"
)

func TestTrackerConcurrentAccess(t *testing.T) {
	tracker := NewTracker(1000, 1.0, 2.0)
	const goroutines = 16
	const addsPer = 50

	var wg sync.WaitGroup
	errs := make(chan error, goroutines*4)

	worker := func(fn func()) {
		defer wg.Done()
		fn()
	}

	for i := 0; i < goroutines; i++ {
		wg.Add(4)
		go worker(func() {
			for j := 0; j < addsPer; j++ {
				tracker.Add(llm.Usage{PromptTokens: 1, CompletionTokens: 2})
			}
		})
		go worker(func() {
			for j := 0; j < addsPer; j++ {
				_ = tracker.Status()
			}
		})
		go worker(func() {
			for j := 0; j < addsPer; j++ {
				if err := tracker.Check(); err != nil {
					errs <- err
				}
			}
		})
		go worker(func() {
			for j := 0; j < addsPer; j++ {
				_ = tracker.CurrentCostUSD()
				tracker.UpdatePrices(1.5, 2.5)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("unexpected budget check failure: %v", err)
		}
	}

	tracker.UpdatePrices(10.0, 20.0)
	wantPrompt := goroutines * addsPer
	wantCompletion := goroutines * addsPer * 2
	// Internal totals: prompt 1 per add, completion 2 per add
	got := tracker.CurrentCostUSD()
	want := (float64(wantPrompt)/1_000_000.0)*10.0 + (float64(wantCompletion)/1_000_000.0)*20.0
	if got != want {
		t.Fatalf("cost = %v, want %v (prompt %d, completion %d)", got, want, wantPrompt, wantCompletion)
	}
}
