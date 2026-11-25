package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pulse-windows/internal/model"
)

func fetchCodex(ctx context.Context, client *http.Client) model.ProviderUsage {
	home, _ := os.UserHomeDir()
	authPath := filepath.Join(home, ".codex", "auth.json")
	raw, err := os.ReadFile(authPath)
	if err != nil {
		return model.UnavailableUsage(model.ProviderCodex, model.ReasonSignInRequired)
	}
	var authDoc struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(raw, &authDoc) != nil || strings.TrimSpace(authDoc.Tokens.AccessToken) == "" {
		return model.UnavailableUsage(model.ProviderCodex, model.ReasonSignInRequired)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://chatgpt.com/backend-api/wham/usage", nil)
	req.Header.Set("Authorization", "Bearer "+authDoc.Tokens.AccessToken)
	if authDoc.Tokens.AccountID != "" {
		req.Header.Set("ChatGPT-Account-Id", authDoc.Tokens.AccountID)
	}
	req.Header.Set("Accept", "application/json")

	status, body, err := doRequest(ctx, client, req)
	if err != nil {
		return model.UnavailableUsage(model.ProviderCodex, model.ReasonUnreachable)
	}
	switch status {
	case 200:
	case 401, 403:
		return model.UnavailableUsage(model.ProviderCodex, model.ReasonSignInRequired)
	case 429:
		return model.UnavailableUsage(model.ProviderCodex, model.ReasonRateLimited)
	default:
		return model.UnavailableUsage(model.ProviderCodex, model.ReasonServerError)
	}

	var root map[string]any
	if json.Unmarshal(body, &root) != nil {
		return model.UnavailableUsage(model.ProviderCodex, model.ReasonUnreadableReply)
	}

	var windows []model.UsageWindow
	spendReached := false
	if sc, ok := root["spend_control"].(map[string]any); ok {
		spendReached, _ = sc["reached"].(bool)
	}
	if lim, ok := root["rate_limit"].(map[string]any); ok {
		group := parseCodexGroup(lim, "codex", "")
		groupSpent := isCodexGroupSpent(lim) || (root["rate_limit_reached_type"] != nil) || spendReached
		if groupSpent {
			markCodexFullestSpent(group)
		}
		windows = append(windows, group...)
	}
	if extras, ok := root["additional_rate_limits"].([]any); ok {
		for _, item := range extras {
			ex, _ := item.(map[string]any)
			if ex == nil {
				continue
			}
			lim, _ := ex["rate_limit"].(map[string]any)
			if lim == nil {
				continue
			}
			label, _ := ex["limit_name"].(string)
			key, _ := ex["metered_feature"].(string)
			if key == "" {
				key = label
			}
			if key == "" {
				key = "extra"
			}
			group := parseCodexGroup(lim, key, label)
			if isCodexGroupSpent(lim) {
				markCodexFullestSpent(group)
			}
			windows = append(windows, group...)
		}
	}
	if len(windows) == 0 {
		return model.UnavailableUsage(model.ProviderCodex, model.ReasonNoLimitsReported)
	}

	plan, _ := root["plan_type"].(string)
	plan = tidyCodexPlan(plan)
	var balanceStr string
	var credRem *model.CreditAmount
	if creds, ok := root["credits"].(map[string]any); ok {
		if bal, ok := toFloat(creds["balance"]); ok && bal >= 0 {
			credRem = &model.CreditAmount{Amount: bal, Currency: "USD"}
			balanceStr = credRem.FormatShort()
		}
	}

	now := time.Now()
	return model.ProviderUsage{
		Provider:        model.ProviderCodex,
		Windows:         windows,
		ObservedAt:      &now,
		State:           model.StateLive,
		Plan:            plan,
		CreditBalance:   balanceStr,
		CreditRemaining: credRem,
		Source:          "endpoint",
	}
}

func parseCodexGroup(lim map[string]any, idPrefix, scope string) []model.UsageWindow {
	var out []model.UsageWindow
	for _, slot := range []string{"primary_window", "secondary_window"} {
		node, _ := lim[slot].(map[string]any)
		if node == nil {
			continue
		}
		pct, ok := toFloat(node["used_percent"])
		if !ok {
			continue
		}
		secsF, _ := toFloat(node["limit_window_seconds"])
		secs := int(secsF)
		var resets *time.Time
		if ts, ok := toFloat(node["reset_at"]); ok && ts > 0 {
			t := time.Unix(int64(ts), 0)
			resets = &t
		}
		out = append(out, model.UsageWindow{
			ID:            idPrefix + "." + slot,
			Kind:          codexKind(secs),
			Scope:         scope,
			UsedFraction:  clampFraction(pct / 100),
			WindowSeconds: secs,
			ResetsAt:      resets,
			ReportsLength: secs > 0,
		})
	}
	return out
}

func codexKind(secs int) model.WindowKind {
	switch secs {
	case 5 * 3600:
		return model.KindFiveHour
	case 7 * 86400:
		return model.KindWeekly
	default:
		return model.KindOther
	}
}

func isCodexGroupSpent(lim map[string]any) bool {
	if lr, _ := lim["limit_reached"].(bool); lr {
		return true
	}
	if al, ok := lim["allowed"].(bool); ok && !al {
		return true
	}
	return false
}

func markCodexFullestSpent(windows []model.UsageWindow) {
	if len(windows) == 0 {
		return
	}
	best := 0
	for i := 1; i < len(windows); i++ {
		if windows[i].UsedFraction > windows[best].UsedFraction {
			best = i
		}
	}
	windows[best].IsExhausted = true
}

func tidyCodexPlan(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	switch strings.ToLower(raw) {
	case "plus":
		return "Plus"
	case "pro":
		return "Pro"
	case "team":
		return "Team"
	case "enterprise":
		return "Enterprise"
	case "free":
		return "Free"
	default:
		return tidyTitle(raw)
	}
}
