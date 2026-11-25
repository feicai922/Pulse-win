package providers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"pulse-windows/internal/model"
	"pulse-windows/internal/win32"
)

func TestPercentRoundingRule(t *testing.T) {
	cases := []struct {
		used      float64
		remaining bool
		want      int
	}{
		{0.0, false, 0},
		{0.0003, false, 1},  // anything used > 0 never reads 0%
		{0.996, false, 99},  // anything < 100% never reads 100%
		{1.0, false, 100},
		{0.996, true, 1},    // anything left > 0 never reads 0%
		{0.004, true, 99},   // anything used > 0 never reads 100% left
		{0.0, true, 100},
		{1.0, true, 0},
	}
	for _, tc := range cases {
		w := model.UsageWindow{UsedFraction: tc.used}
		got := w.PercentValue(tc.remaining)
		if got != tc.want {
			t.Errorf("used=%.4f remaining=%v: got %d, want %d", tc.used, tc.remaining, got, tc.want)
		}
	}
}

func TestParseClaudeWindows(t *testing.T) {
	raw := `{
		"limits": [
			{"kind":"session","percent":24.5,"severity":"normal","resets_at":"2026-11-25T12:00:00Z"},
			{"kind":"weekly_scoped","percent":80,"severity":"warning","scope":{"model":{"display_name":"Sonnet"}},"resets_at":"2026-12-01T00:00:00Z"}
		]
	}`
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		t.Fatal(err)
	}
	wins := parseClaudeWindows(root)
	if len(wins) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(wins))
	}
	if wins[0].Kind != model.KindFiveHour || wins[0].IsExhausted {
		t.Fatalf("unexpected window 0: %+v", wins[0])
	}
	if wins[1].Scope != "Sonnet" || wins[1].IsExhausted {
		t.Fatalf("warning severity should not mark exhausted: %+v", wins[1])
	}
}

func TestParseAntigravityWindows(t *testing.T) {
	raw := `{
		"response": {
			"groups": [
				{
					"displayName": "Gemini Models",
					"buckets": [
						{"bucketId":"b1","window":"5h","remainingFraction":0.75,"resetTime":"2026-11-25T15:00:00Z"}
					]
				}
			]
		}
	}`
	var reply antigravityQuotaReply
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatal(err)
	}
	wins := parseAntigravityWindows(reply)
	if len(wins) != 1 {
		t.Fatalf("expected 1 window, got %d", len(wins))
	}
	if wins[0].Scope != "Gemini" {
		t.Errorf("expected scope 'Gemini', got %q", wins[0].Scope)
	}
	if wins[0].UsedFraction < 0.249 || wins[0].UsedFraction > 0.251 {
		t.Errorf("expected usedFraction 0.25, got %v", wins[0].UsedFraction)
	}
}

func TestParseCursorWindows(t *testing.T) {
	raw := `{
		"billingCycleStart": "2026-11-01T00:00:00Z",
		"billingCycleEnd": "2026-12-01T00:00:00Z",
		"membershipType": "pro_plus",
		"individualUsage": {
			"plan": {"autoPercentUsed": 12.5, "apiPercentUsed": 40, "remaining": 1500}
		}
	}`
	var reply cursorSummaryReply
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatal(err)
	}
	wins := cursorWindowsFromSummary(reply)
	if len(wins) != 2 {
		t.Fatalf("expected 2 pools, got %d", len(wins))
	}
	if cursorPlanName(reply.MembershipType) != "Pro+" {
		t.Errorf("expected Pro+, got %q", cursorPlanName(reply.MembershipType))
	}
}

func TestApplyBalanceRing(t *testing.T) {
	now := time.Now()
	u := model.ProviderUsage{
		Provider:        model.ProviderDeepSeek,
		State:           model.StateLive,
		ObservedAt:      &now,
		CreditRemaining: &model.CreditAmount{Amount: 75, Currency: "CNY"},
	}
	res := ApplyBalanceRing(u, model.BasisYourBudget, 100)
	if len(res.Windows) != 1 {
		t.Fatalf("expected 1 balance window, got %d", len(res.Windows))
	}
	if res.Windows[0].UsedFraction != 0.25 || res.Windows[0].Estimate != model.EstimateYourBudget {
		t.Fatalf("unexpected balance window: %+v", res.Windows[0])
	}
}

func TestAgyVersionCheck(t *testing.T) {
	if !agyAnswersUsageItself("1.3.5") {
		t.Fatal("expected 1.3.5 to support /usage")
	}
	if agyAnswersUsageItself("1.1.10") {
		t.Fatal("expected 1.1.10 to be too old")
	}
}

func TestMergePreferredPort(t *testing.T) {
	got := mergePreferredPort(64347, []int{58254, 64347, 64348})
	if len(got) != 3 || got[0] != 64347 {
		t.Fatalf("expected 64347 prioritized, got %v", got)
	}
	if got2 := mergePreferredPort(9999, []int{58254, 64347}); len(got2) != 2 || got2[0] != 58254 {
		t.Fatalf("unexpected non-live preferred port inclusion: %v", got2)
	}
}

func TestLiveAntigravityProbe(t *testing.T) {
	procs := win32.FindAntigravityServers()
	t.Logf("found %d antigravity language_server processes", len(procs))
	for _, p := range procs {
		tok := extractCSRFToken(p.CmdLine, p.Env)
		ports := win32.ListeningPortsForPID(p.PID)
		t.Logf("pid=%d hasToken=%v ports=%v", p.PID, tok != "", ports)
	}
	if len(procs) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		u := fetchAntigravity(ctx)
		t.Logf("fetchAntigravity state=%s source=%s plan=%q windows=%d reason=%s",
			u.State, u.Source, u.Plan, len(u.Windows), u.Reason)
	}
}
