package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"pulse-windows/internal/model"
)

var (
	claudePlanMu    sync.Mutex
	claudePlanCache = map[string]struct {
		name string
		at   time.Time
	}{}
)

func fetchClaudeCode(ctx context.Context, client *http.Client) model.ProviderUsage {
	home, _ := os.UserHomeDir()
	credPath := filepath.Join(home, ".claude", ".credentials.json")
	raw, err := os.ReadFile(credPath)
	if err != nil {
		return model.UnavailableUsage(model.ProviderClaudeCode, model.ReasonClaudeSignInRequired)
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return model.UnavailableUsage(model.ProviderClaudeCode, model.ReasonClaudeSignInRequired)
	}
	oauth, _ := doc["claudeAiOauth"].(map[string]any)
	if oauth == nil {
		return model.UnavailableUsage(model.ProviderClaudeCode, model.ReasonClaudeSignInRequired)
	}
	token, _ := oauth["accessToken"].(string)
	token = strings.TrimSpace(token)
	if token == "" {
		return model.UnavailableUsage(model.ProviderClaudeCode, model.ReasonClaudeSignInRequired)
	}
	if expMs, ok := toFloat(oauth["expiresAt"]); ok && expMs > 0 {
		if time.UnixMilli(int64(expMs)).Before(time.Now()) {
			return model.UnavailableUsage(model.ProviderClaudeCode, model.ReasonClaudeLoginExpired)
		}
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.anthropic.com/api/oauth/usage", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "claude-cli (external, cli)")
	req.Header.Set("Accept", "application/json")

	status, body, err := doRequest(ctx, client, req)
	if err != nil {
		return model.UnavailableUsage(model.ProviderClaudeCode, model.ReasonUnreachable)
	}
	switch status {
	case 200:
	case 401, 403:
		return model.UnavailableUsage(model.ProviderClaudeCode, model.ReasonClaudeLoginExpired)
	case 429:
		return model.UnavailableUsage(model.ProviderClaudeCode, model.ReasonRateLimited)
	default:
		return model.UnavailableUsage(model.ProviderClaudeCode, model.ReasonServerError)
	}

	var root map[string]any
	if json.Unmarshal(body, &root) != nil {
		return model.UnavailableUsage(model.ProviderClaudeCode, model.ReasonUnreadableReply)
	}

	plan := getClaudePlan(ctx, client, token)
	now := time.Now()
	windows := parseClaudeWindows(root)
	if len(windows) == 0 {
		return model.UnavailableUsage(model.ProviderClaudeCode, model.ReasonNoLimitsReported)
	}
	return model.ProviderUsage{
		Provider:   model.ProviderClaudeCode,
		Windows:    windows,
		ObservedAt: &now,
		State:      model.StateLive,
		Plan:       plan,
		Source:     "endpoint",
	}
}

func parseClaudeWindows(root map[string]any) []model.UsageWindow {
	var windows []model.UsageWindow
	if limits, ok := root["limits"].([]any); ok {
		for _, item := range limits {
			lim, _ := item.(map[string]any)
			if lim == nil {
				continue
			}
			kindName, _ := lim["kind"].(string)
			pct, ok := toFloat(lim["percent"])
			if !ok {
				continue
			}
			var kind model.WindowKind
			var secs int
			switch kindName {
			case "session":
				kind = model.KindFiveHour
				secs = 5 * 3600
			case "weekly_all", "weekly_scoped":
				kind = model.KindWeekly
				secs = 7 * 86400
			default:
				continue
			}
			var scope string
			if sc, ok := lim["scope"].(map[string]any); ok {
				if mod, ok := sc["model"].(map[string]any); ok {
					scope, _ = mod["display_name"].(string)
				}
			}
			idScope := scope
			if idScope == "" {
				idScope = "all"
			}
			resetsStr, _ := lim["resets_at"].(string)
			windows = append(windows, model.UsageWindow{
				ID:            "claudeCode." + kindName + "." + idScope,
				Kind:          kind,
				Scope:         scope,
				UsedFraction:  clampFraction(pct / 100),
				WindowSeconds: secs,
				ResetsAt:      parseISO8601(resetsStr),
				ReportsLength: true,
				IsExhausted:   isClaudeSpent(lim),
			})
		}
	}
	if len(windows) == 0 {
		pairs := []struct {
			key  string
			kind model.WindowKind
			sec  int
		}{
			{"five_hour", model.KindFiveHour, 5 * 3600},
			{"seven_day", model.KindWeekly, 7 * 86400},
		}
		for _, p := range pairs {
			node, _ := root[p.key].(map[string]any)
			if node == nil {
				continue
			}
			pct, ok := toFloat(node["utilization"])
			if !ok {
				continue
			}
			resetsStr, _ := node["resets_at"].(string)
			windows = append(windows, model.UsageWindow{
				ID:            "claudeCode." + p.key,
				Kind:          p.kind,
				UsedFraction:  clampFraction(pct / 100),
				WindowSeconds: p.sec,
				ResetsAt:      parseISO8601(resetsStr),
				ReportsLength: true,
				IsExhausted:   isClaudeSpent(node),
			})
		}
	}
	return windows
}

func isClaudeSpent(lim map[string]any) bool {
	if lr, ok := lim["locked_reason"]; ok && lr != nil {
		return true
	}
	sev, _ := lim["severity"].(string)
	if sev == "" {
		return false
	}
	switch strings.ToLower(sev) {
	case "normal", "ok", "none", "healthy", "warning", "warn":
		return false
	default:
		return true
	}
}

func getClaudePlan(ctx context.Context, client *http.Client, token string) string {
	claudePlanMu.Lock()
	if c, ok := claudePlanCache["primary"]; ok && time.Since(c.at) < 6*time.Hour {
		claudePlanMu.Unlock()
		return c.name
	}
	claudePlanMu.Unlock()

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.anthropic.com/api/oauth/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "claude-cli (external, cli)")
	req.Header.Set("Accept", "application/json")
	status, body, err := doRequest(ctx, client, req)
	var name string
	if err == nil && status == 200 {
		var root map[string]any
		if json.Unmarshal(body, &root) == nil {
			name = parseClaudePlanName(root)
		}
	}
	claudePlanMu.Lock()
	claudePlanCache["primary"] = struct {
		name string
		at   time.Time
	}{name: name, at: time.Now()}
	claudePlanMu.Unlock()
	return name
}

func parseClaudePlanName(root map[string]any) string {
	org, _ := root["organization"].(map[string]any)
	tier := ""
	if org != nil {
		tier, _ = org["rate_limit_tier"].(string)
	}
	lowerTier := strings.ToLower(tier)
	mult := ""
	if strings.HasSuffix(lowerTier, "_20x") {
		mult = "20x"
	} else if strings.HasSuffix(lowerTier, "_5x") {
		mult = "5x"
	}

	stated := ""
	if org != nil {
		stated, _ = org["subscription_type"].(string)
	}
	if stated == "" {
		stated, _ = root["subscription_type"].(string)
	}
	if stated != "" {
		n := tidyTitle(stated)
		if mult != "" {
			return n + " " + mult
		}
		return n
	}
	if lowerTier == "" {
		if org != nil {
			if ot, _ := org["organization_type"].(string); ot != "" {
				return tidyTitle(ot)
			}
		}
		return ""
	}
	base := tidyTitle(lowerTier)
	switch {
	case strings.Contains(lowerTier, "max"):
		base = "Max"
	case strings.Contains(lowerTier, "team"):
		base = "Team"
	case strings.Contains(lowerTier, "enterprise"):
		base = "Enterprise"
	case strings.Contains(lowerTier, "pro"):
		base = "Pro"
	case strings.Contains(lowerTier, "free"):
		base = "Free"
	}
	if mult != "" {
		return base + " " + mult
	}
	return base
}

func tidyTitle(raw string) string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == '_' || r == '-' })
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

func clampFraction(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
