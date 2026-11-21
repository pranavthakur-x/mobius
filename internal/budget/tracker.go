package budget

import (
	"fmt"
	"mobius/pkg/llm"
	"sync"
)

// USDToINRRate defines the static USD-to-INR conversion factor used for display formatting.
const USDToINRRate = 95

// CostTracker records LLM token usage and enforces spending limits.
type CostTracker interface {
	Add(u llm.Usage)
	Check() error
	Status() string
	CurrentCostUSD() float64
	UpdatePrices(pricePrompt, priceComp float64)
}

// Tracker keeps a running total of tokens and calculates USD costs.
type Tracker struct {
	mu sync.RWMutex

	totalPromptTokens     int
	totalCompletionTokens int
	maxCost               float64

	pricePerMillionPrompt     float64
	pricePerMillionCompletion float64
}

// NewTracker initializes a fresh tracker starting at 0 tokens.
func NewTracker(maxCost, pricePrompt, priceComp float64) *Tracker {
	return &Tracker{
		maxCost:                   maxCost,
		pricePerMillionPrompt:     pricePrompt,
		pricePerMillionCompletion: priceComp,
	}
}

// Add tallies up the tokens from a single LLM generation.
func (t *Tracker) Add(u llm.Usage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.totalPromptTokens += u.PromptTokens
	t.totalCompletionTokens += u.CompletionTokens
}

func (t *Tracker) currentCostUSDLocked() float64 {
	promptCost := (float64(t.totalPromptTokens) / 1_000_000.0) * t.pricePerMillionPrompt
	compCost := (float64(t.totalCompletionTokens) / 1_000_000.0) * t.pricePerMillionCompletion
	return promptCost + compCost
}

func (t *Tracker) currentCostINRLocked() float64 {
	return t.currentCostUSDLocked() * USDToINRRate
}

// CurrentCostUSD calculates the running total spend in USD.
func (t *Tracker) CurrentCostUSD() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.currentCostUSDLocked()
}

// CurrentCostINR converts the current USD spend into Indian Rupees.
func (t *Tracker) CurrentCostINR() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.currentCostINRLocked()
}

// Check aborts if we've spent more than the max allowed cost.
func (t *Tracker) Check() error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	cost := t.currentCostUSDLocked()
	if cost > t.maxCost {
		return fmt.Errorf("budget exceeded: current cost $%.4f (₹%.2f) > max cost $%.2f", cost, t.currentCostINRLocked(), t.maxCost)
	}
	return nil
}

// Status formats a summary for the terminal showing both USD and INR.
func (t *Tracker) Status() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.pricePerMillionPrompt == 0 && t.pricePerMillionCompletion == 0 {
		return fmt.Sprintf("[Budget] Tokens: %d In, %d Out", t.totalPromptTokens, t.totalCompletionTokens)
	}
	return fmt.Sprintf("[Budget] Tokens: %d In, %d Out | Cost: $%.5f (₹%.4f)",
		t.totalPromptTokens, t.totalCompletionTokens, t.currentCostUSDLocked(), t.currentCostINRLocked())
}

// UpdatePrices updates the per-million prompt and completion token rates in USD.
func (t *Tracker) UpdatePrices(pricePrompt, priceComp float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pricePerMillionPrompt = pricePrompt
	t.pricePerMillionCompletion = priceComp
}
