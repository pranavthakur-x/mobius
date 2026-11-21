package tui

import (
	"context"
	"sync"
	"testing"
)

func TestShutdownNoSendOnClosedChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan LiveEvent, 4)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ch:
			case <-ctx.Done():
				return
			}
		}
	}()
	cancel()
	send := func(ev LiveEvent) {
		select {
		case ch <- ev:
		case <-ctx.Done():
		}
	}
	for i := 0; i < 100; i++ {
		send(BudgetEvent{SourceRef: SourceRef{ThreadID: "t"}})
	}
	wg.Wait()
}
