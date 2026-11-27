package model

import "testing"

func TestDefaultEnabledProviders(t *testing.T) {
	s := DefaultSettings()
	got := s.OrderedEnabled()
	if len(got) != 2 || got[0] != ProviderCodex || got[1] != ProviderAntigravity {
		t.Fatalf("expected default enabled [codex, antigravity], got %v", got)
	}
}

func TestUpdateAndSetEnabledNoDeadlock(t *testing.T) {
	s := DefaultSettings()
	s.Update(func(cfg *AppSettings) {
		cfg.SetEnabled(ProviderClaudeCode, true)
		cfg.SetEnabled(ProviderCodex, true)
	})
	if !s.IsEnabled(ProviderClaudeCode) || !s.IsEnabled(ProviderCodex) {
		t.Fatal("expected both providers to be enabled")
	}
	s.Update(func(cfg *AppSettings) {
		cfg.SetEnabled(ProviderClaudeCode, false)
	})
	if s.IsEnabled(ProviderClaudeCode) {
		t.Fatal("expected ClaudeCode to be disabled")
	}
}
