package model

import "testing"

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
