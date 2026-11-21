package orchestrator

import (
	"strings"
	"testing"
)

func TestParseDecisionPayload(t *testing.T) {
	valid := `{"think":"t","action":"reply","reply":"ok","team_id":"","role_id":"","task":""}`
	cases := []struct {
		name    string
		raw     string
		wantErr string
		wantAct string
	}{
		{name: "plain", raw: valid, wantAct: "reply"},
		{name: "fenced", raw: "```json\n" + valid + "\n```", wantAct: "reply"},
		{name: "prose wrapped", raw: "Sure.\n" + valid + "\nDone.", wantAct: "reply"},
		{name: "empty", raw: "", wantErr: "empty content"},
		{name: "no object", raw: "just text", wantErr: "no JSON object"},
		{name: "partial", raw: `{"think":"x"`, wantErr: "invalid routing JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := parseDecisionPayload(tc.raw)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if d.Action != tc.wantAct {
				t.Fatalf("action = %q", d.Action)
			}
		})
	}
}

func TestExtractJSONObject(t *testing.T) {
	in := `prefix {"a":"1","b":"2"} suffix`
	got := extractJSONObject(in)
	if got != `{"a":"1","b":"2"}` {
		t.Fatalf("got %q", got)
	}
}

func TestDecideRepairOnBadJSON(t *testing.T) {
	chdirModuleRoot(t)
	good := `{"think":"t","action":"reply","reply":"hi","team_id":"","role_id":"","task":""}`
	provider := &recordingProvider{
		replies: []string{"not json", good},
	}
	ws := newTestWorkspace(t, provider)
	_, err := ws.Handle(t.Context(), "hello", DispatchSync)
	if err != nil {
		t.Fatal(err)
	}
	if provider.orchestratorCalls() != 2 {
		t.Fatalf("expected 2 orchestrator calls (repair), got %d", provider.orchestratorCalls())
	}
}
