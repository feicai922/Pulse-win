package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"pulse-windows/internal/model"
)

func fetchCopilot(ctx context.Context, client *http.Client, token string) model.ProviderUsage {
	token = strings.TrimSpace(token)
	if token == "" {
		return model.UnavailableUsage(model.ProviderCopilot, model.ReasonSignedOut)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/copilot_internal/user", nil)
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Github-Api-Version", "2025-04-01")
	req.Header.Set("Editor-Version", "vscode/1.96.2")
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/0.26.7")

	status, body, err := doRequest(ctx, client, req)
	if err != nil {
		return model.UnavailableUsage(model.ProviderCopilot, model.ReasonUnreachable)
	}
	switch status {
	case 200:
	case 401, 403:
		return model.UnavailableUsage(model.ProviderCopilot, model.ReasonSignedOut)
	case 429:
		return model.UnavailableUsage(model.ProviderCopilot, model.ReasonRateLimited)
	default:
		return model.UnavailableUsage(model.ProviderCopilot, model.ReasonServerError)
	}

	var root map[string]any
	if json.Unmarshal(body, &root) != nil {
		return model.UnavailableUsage(model.ProviderCopilot, model.ReasonUnreadableReply)
	}

	snapshots, _ := root["quota_snapshots"].(map[string]any)
	resetStr, _ := root["quota_reset_date_utc"].(string)
	if resetStr == "" {
		resetStr, _ = root["quota_reset_date"].(string)
	}
	resets := parseISO8601(resetStr)

	lanes := []struct {
		key   string
		scope string
	}{
		{"premium_interactions", "Premium requests"},
		{"chat", "Chat"},
		{"completions", "Completions"},
	}

	var windows []model.UsageWindow
	for _, lane := range lanes {
		snap, _ := snapshots[lane.key].(map[string]any)
		if snap == nil {
			continue
		}
		entitlement, _ := toFloat(snap["entitlement"])
		unlimited, _ := snap["unlimited"].(bool)
		creditsUsed, _ := toFloat(snap["credits_used"])
		if hasQuota, ok := snap["has_quota"].(bool); ok && !hasQuota && entitlement <= 0 && creditsUsed <= 0 {
			continue
		}
		if _, hasQuotaKey := snap["has_quota"]; !hasQuotaKey && !unlimited && entitlement <= 0 {
			remCount, _ := toFloat(snap["remaining"])
			remPct, _ := toFloat(snap["percent_remaining"])
			if remCount <= 0 && remPct >= 100 {
				continue
			}
		}
		if unlimited {
			continue
		}
		remPct, ok := toFloat(snap["percent_remaining"])
		if !ok {
			continue
		}
		overagePermitted, _ := snap["overage_permitted"].(bool)

		var amounts *model.UsageAmounts
		if entitlement > 0 {
			remVal, okRem := toFloat(snap["quota_remaining"])
			if !okRem {
				remVal, okRem = toFloat(snap["remaining"])
			}
			if okRem && remVal >= 0 && remVal <= entitlement {
				r := remVal
				amounts = &model.UsageAmounts{
					Used:      entitlement - remVal,
					Remaining: &r,
					Limit:     entitlement,
				}
			}
		}

		windows = append(windows, model.UsageWindow{
			ID:            "copilot." + lane.key,
			Kind:          model.KindMonthly,
			Scope:         lane.scope,
			UsedFraction:  clampFraction((100 - remPct) / 100),
			WindowSeconds: 30 * 86400,
			ResetsAt:      resets,
			ReportsLength: false,
			IsExhausted:   remPct <= 0 && !overagePermitted,
			Amounts:       amounts,
		})
	}

	if len(windows) == 0 {
		return model.UnavailableUsage(model.ProviderCopilot, model.ReasonNoLimitsReported)
	}

	plan, _ := root["copilot_plan"].(string)
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case "individual":
		plan = "Individual"
	case "free":
		plan = "Free"
	case "business":
		plan = "Business"
	case "enterprise":
		plan = "Enterprise"
	}

	now := time.Now()
	return model.ProviderUsage{
		Provider:   model.ProviderCopilot,
		Windows:    windows,
		ObservedAt: &now,
		State:      model.StateLive,
		Plan:       plan,
		Source:     "endpoint",
	}
}
