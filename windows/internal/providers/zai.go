package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pulse-windows/internal/model"
)

func fetchZai(ctx context.Context, client *http.Client, provider model.ProviderID, enteredKey string) model.ProviderUsage {
	key := strings.TrimSpace(enteredKey)
	if key == "" && provider == model.ProviderGLMCoding {
		key = storedGLMKey()
	}
	if key == "" {
		return model.UnavailableUsage(provider, model.ReasonAPIKeyMissing)
	}

	host := "https://api.z.ai"
	if provider == model.ProviderGLMCoding {
		host = "https://open.bigmodel.cn"
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", host+"/api/monitor/usage/quota/limit", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")

	status, body, err := doRequest(ctx, client, req)
	if err != nil {
		return model.UnavailableUsage(provider, model.ReasonUnreachable)
	}
	switch status {
	case 200:
	case 401, 403:
		return model.UnavailableUsage(provider, model.ReasonAPIKeyRefused)
	case 429:
		return model.UnavailableUsage(provider, model.ReasonRateLimited)
	default:
		return model.UnavailableUsage(provider, model.ReasonServerError)
	}

	var reply struct {
		Success bool   `json:"success"`
		Code    int    `json:"code"`
		Msg     string `json:"msg"`
		Data    *struct {
			PlanName    string `json:"planName"`
			Plan        string `json:"plan"`
			PlanType    string `json:"plan_type"`
			PackageName string `json:"packageName"`
			Level       string `json:"level"`
			Limits      []struct {
				Type          string   `json:"type"`
				Unit          int      `json:"unit"`
				Number        int      `json:"number"`
				Percentage    *float64 `json:"percentage"`
				Usage         *float64 `json:"usage"`
				CurrentValue  *float64 `json:"currentValue"`
				Remaining     *float64 `json:"remaining"`
				NextResetTime *float64 `json:"nextResetTime"`
			} `json:"limits"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &reply) != nil {
		return model.UnavailableUsage(provider, model.ReasonUnreadableReply)
	}
	if !reply.Success || reply.Code != 200 {
		return model.UnavailableUsage(provider, classifyZaiProblem(reply.Code, reply.Msg))
	}
	if reply.Data == nil {
		return model.UnavailableUsage(provider, model.ReasonNoLimitsReported)
	}

	var windows []model.UsageWindow
	for idx, lim := range reply.Data.Limits {
		if lim.Type != "TOKENS_LIMIT" && lim.Type != "CREDIT_LIMIT" && lim.Type != "TIME_LIMIT" {
			continue
		}
		mins := zaiMinutes(lim.Unit, lim.Number, lim.Type)
		if mins <= 0 {
			continue
		}
		var pct float64
		hasPct := false
		if lim.Usage != nil && *lim.Usage > 0 {
			u := *lim.Usage
			var used float64
			if lim.Remaining != nil {
				remDiff := u - *lim.Remaining
				used = remDiff
				if lim.CurrentValue != nil && *lim.CurrentValue > remDiff {
					used = *lim.CurrentValue
				}
				hasPct = true
			} else if lim.CurrentValue != nil {
				used = *lim.CurrentValue
				hasPct = true
			}
			if hasPct {
				if used > u {
					used = u
				}
				pct = clampFraction(used/u) * 100
			}
		}
		if !hasPct && lim.Percentage != nil {
			pct = *lim.Percentage
			hasPct = true
		}
		if !hasPct {
			continue
		}

		kind := model.KindOther
		switch mins {
		case 300:
			kind = model.KindFiveHour
		case 10080:
			kind = model.KindWeekly
		case 43200:
			kind = model.KindMonthly
		}
		scope := ""
		if lim.Type == "TIME_LIMIT" {
			scope = "MCP"
		}
		var resets *time.Time
		if lim.NextResetTime != nil && *lim.NextResetTime > 0 {
			t := time.UnixMilli(int64(*lim.NextResetTime))
			resets = &t
		}
		windows = append(windows, model.UsageWindow{
			ID:            fmt.Sprintf("%s.%s.%d-%d.%d", provider, lim.Type, lim.Unit, lim.Number, idx),
			Kind:          kind,
			Scope:         scope,
			UsedFraction:  clampFraction(pct / 100),
			WindowSeconds: mins * 60,
			ResetsAt:      resets,
			ReportsLength: true,
			IsExhausted:   pct >= 100,
		})
	}
	sort.SliceStable(windows, func(i, j int) bool {
		return windows[i].WindowSeconds < windows[j].WindowSeconds
	})
	if len(windows) == 0 {
		return model.UnavailableUsage(provider, model.ReasonNoLimitsReported)
	}

	plan := ""
	for _, cand := range []string{reply.Data.PlanName, reply.Data.Plan, reply.Data.PlanType, reply.Data.PackageName, reply.Data.Level} {
		if t := strings.TrimSpace(cand); t != "" {
			plan = t
			break
		}
	}
	now := time.Now()
	return model.ProviderUsage{
		Provider:   provider,
		Windows:    windows,
		ObservedAt: &now,
		State:      model.StateLive,
		Plan:       plan,
		Source:     "endpoint",
	}
}

func zaiMinutes(unit, num int, limType string) int {
	if limType == "TIME_LIMIT" && unit == 5 && num == 1 {
		return 30 * 24 * 60
	}
	if num <= 0 {
		return 0
	}
	perUnit := map[int]int{1: 1440, 3: 60, 5: 1, 6: 10080}[unit]
	return num * perUnit
}

func classifyZaiProblem(code int, msg string) model.Unavailability {
	said := strings.ToLower(msg)
	if strings.Contains(said, "coding plan") {
		return model.ReasonNoPlan
	}
	for _, kw := range []string{"token", "auth", "key", "unauthor", "forbidden", "credential", "身份验证", "鉴权", "认证", "令牌", "未授权", "无权限", "密钥"} {
		if strings.Contains(said, kw) {
			return model.ReasonAPIKeyRefused
		}
	}
	if code == 401 || code == 403 || (code >= 1000 && code <= 1099) {
		return model.ReasonAPIKeyRefused
	}
	if code == 429 {
		return model.ReasonRateLimited
	}
	return model.ReasonServerError
}

func storedGLMKey() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".coding-relay", "glm-api-key"),
		filepath.Join(home, ".config", "bigmodel", "api_key"),
		filepath.Join(home, ".config", "zhipu", "api_key"),
	}
	for _, p := range candidates {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
		if len(lines) > 0 {
			if k := strings.TrimSpace(lines[0]); k != "" {
				return k
			}
		}
	}
	return ""
}
