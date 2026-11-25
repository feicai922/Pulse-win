package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"pulse-windows/internal/model"
)

func fetchMiniMax(ctx context.Context, client *http.Client, site, apiKey string) model.ProviderUsage {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return model.UnavailableUsage(model.ProviderMiniMax, model.ReasonAPIKeyMissing)
	}
	host := "https://api.minimax.io"
	if site == "china" {
		host = "https://api.minimaxi.com"
	}
	endpoints := []string{
		host + "/v1/token_plan/remains",
		host + "/v1/api/openplatform/coding_plan/remains",
	}

	var firstProblem model.Unavailability
	for _, ep := range endpoints {
		req, _ := http.NewRequestWithContext(ctx, "GET", ep, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Accept", "application/json")

		status, body, err := doRequest(ctx, client, req)
		if err != nil {
			if firstProblem == "" {
				firstProblem = model.ReasonUnreachable
			}
			continue
		}
		if status == 404 {
			continue
		}
		if status == 401 || status == 403 {
			if firstProblem == "" {
				firstProblem = model.ReasonAPIKeyRefused
			}
			continue
		}
		if status == 429 {
			if firstProblem == "" {
				firstProblem = model.ReasonRateLimited
			}
			continue
		}
		if status != 200 {
			if firstProblem == "" {
				firstProblem = model.ReasonServerError
			}
			continue
		}

		var root map[string]any
		if json.Unmarshal(body, &root) != nil {
			if firstProblem == "" {
				firstProblem = model.ReasonUnreadableReply
			}
			continue
		}
		if base, ok := root["base_resp"].(map[string]any); ok {
			if sc, _ := toFloat(base["status_code"]); sc != 0 {
				sm, _ := base["status_msg"].(string)
				low := strings.ToLower(sm)
				if sc == 1004 || strings.Contains(low, "token") || strings.Contains(low, "auth") || strings.Contains(low, "credential") {
					if firstProblem == "" {
						firstProblem = model.ReasonAPIKeyRefused
					}
				} else if firstProblem == "" {
					firstProblem = model.ReasonServerError
				}
				continue
			}
		}

		payload := root
		if data, ok := root["data"].(map[string]any); ok {
			payload = data
		}
		windows := parseMiniMaxWindows(payload)
		if len(windows) == 0 {
			if firstProblem == "" {
				firstProblem = model.ReasonNoLimitsReported
			}
			continue
		}

		plan := firstNonEmptyStr(payload, "current_subscribe_title", "plan_name", "combo_title", "current_plan_title")
		var balStr string
		for _, k := range []string{"points_balance", "point_balance", "credits_balance", "credit_balance", "balance"} {
			if pts, ok := toFloat(payload[k]); ok && pts > 0 {
				balStr = fmt.Sprintf("%.0f pts", pts)
				break
			}
		}
		now := time.Now()
		return model.ProviderUsage{
			Provider:      model.ProviderMiniMax,
			Windows:       windows,
			ObservedAt:    &now,
			State:         model.StateLive,
			Plan:          plan,
			CreditBalance: balStr,
			Source:        "endpoint",
		}
	}
	if firstProblem == "" {
		firstProblem = model.ReasonUnreachable
	}
	return model.UnavailableUsage(model.ProviderMiniMax, firstProblem)
}

func parseMiniMaxWindows(payload map[string]any) []model.UsageWindow {
	models, _ := payload["model_remains"].([]any)
	var out []model.UsageWindow
	for idx, item := range models {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		name, _ := m["model_name"].(string)
		name = strings.TrimSpace(name)
		scope := name
		if strings.EqualFold(scope, "general") {
			scope = ""
		}
		key := name
		if key == "" {
			key = fmt.Sprintf("lane%d", idx)
		}

		// Interval window
		if !minimaxUnavailable(m["current_interval_status"], m["current_interval_total_count"], m["current_interval_remaining_percent"]) {
			if used, ok := minimaxSpentPct(m["current_interval_remaining_percent"], m["current_interval_total_count"], m["current_interval_usage_count"]); ok {
				startMs, okS := toFloat(m["start_time"])
				endMs, okE := toFloat(m["end_time"])
				if okS && okE && endMs > startMs {
					secs := int((endMs - startMs) / 1000)
					if secs > 0 {
						t := time.UnixMilli(int64(endMs))
						out = append(out, model.UsageWindow{
							ID:            "minimax." + key + ".interval",
							Kind:          codexKind(secs),
							Scope:         scope,
							UsedFraction:  clampFraction(used / 100),
							WindowSeconds: secs,
							ResetsAt:      &t,
							ReportsLength: true,
							IsExhausted:   used >= 100,
						})
					}
				}
			}
		}
		// Weekly window
		if !minimaxUnavailable(m["current_weekly_status"], m["current_weekly_total_count"], m["current_weekly_remaining_percent"]) {
			if used, ok := minimaxSpentPct(m["current_weekly_remaining_percent"], m["current_weekly_total_count"], m["current_weekly_usage_count"]); ok {
				var resets *time.Time
				if endMs, okE := toFloat(m["weekly_end_time"]); okE && endMs > 0 {
					t := time.UnixMilli(int64(endMs))
					resets = &t
				}
				out = append(out, model.UsageWindow{
					ID:            "minimax." + key + ".weekly",
					Kind:          model.KindWeekly,
					Scope:         scope,
					UsedFraction:  clampFraction(used / 100),
					WindowSeconds: 7 * 86400,
					ResetsAt:      resets,
					ReportsLength: true,
					IsExhausted:   used >= 100,
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].WindowSeconds < out[j].WindowSeconds
	})
	return out
}

func minimaxUnavailable(statusV, totalV, remPctV any) bool {
	st, _ := toFloat(statusV)
	tot, _ := toFloat(totalV)
	rem, hasRem := toFloat(remPctV)
	return st == 3 && tot == 0 && (!hasRem || rem >= 100)
}

func minimaxSpentPct(remPctV, totalV, leftV any) (float64, bool) {
	if remPct, ok := toFloat(remPctV); ok {
		v := 100 - remPct
		if v < 0 {
			v = 0
		}
		if v > 100 {
			v = 100
		}
		return v, true
	}
	tot, okT := toFloat(totalV)
	left, okL := toFloat(leftV)
	if okT && tot > 0 && okL {
		v := (tot - left) / tot * 100
		if v < 0 {
			v = 0
		}
		if v > 100 {
			v = 100
		}
		return v, true
	}
	return 0, false
}

func firstNonEmptyStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
