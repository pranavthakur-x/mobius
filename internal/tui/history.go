package tui

// applyHistoryTurn restores one saved line into a chat buffer.
func applyHistoryTurn(buf *ChatBuffer, chatID string, turn HistoryTurn, settings Settings) {
	if buf == nil || turn.Text == "" {
		return
	}
	speaker := turn.Speaker
	if speaker == "" {
		speaker = "orchestrator"
	}
	src := SourceRef{ChatID: chatID, Name: speaker, Kind: SourceOrchestrator}
	switch turn.Role {
	case "user":
		buf.AppendUser(turn.Text, "")
	case "trace":
		buf.ApplyLiveEvent(RouteTraceEvent{SourceRef: src, Text: turn.Text}, settings)
	case "error":
		buf.ApplyLiveEvent(RunErrorEvent{SourceRef: src, Text: turn.Text, Key: "history:" + turn.Text}, settings)
	default:
		buf.ApplyLiveEvent(FinalReplyEvent{SourceRef: src, Text: turn.Text}, settings)
	}
}
