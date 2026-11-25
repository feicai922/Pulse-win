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

func fetchKimiCode(ctx context.Context, client *http.Client, apiKey string) model.ProviderUsage {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return model.UnavailableUsage(model.ProviderKimiCode, model.ReasonAPIKeyMissing)
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.kimi.com/coding/v1/usages", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")

	status, body, err := doRequest(ctx, client, req)
	if err != nil {
		return model.UnavailableUsage(model.ProviderKimiCode, model.ReasonUnreachable)
	}
	switch status {
	case 200:
	case 401, 403:
		return model.UnavailableUsage(model.ProviderKimiCode, model.ReasonAPIKeyRefused)
	case 429:
		return model.UnavailableUsage(model.ProviderKimiCode, model.ReasonRateLimited)
	default:
		return model.UnavailableUsage(model.ProviderKimiCode, model.ReasonServerError)
	}

	var reply struct {
		User *struct {
			Membership *struct {
				Level string `json:"level"`
			} `json:"membership"`
		} `json:"user"`
		Usage  *kimiDetail `json:"usage"`
		Usages *struct {
			MonthCode  *kimiRatio `json:"limit_month_code"`
			MonthTotal *kimiRatio `json:"limit_month_total"`
		} `json:"usages"`
		Limits []struct {
			Window *struct {
				Duration int    `json:"duration"`
				TimeUnit string `json:"timeUnit"`
			} `json:"window"`
			Detail *kimiDetail `json:"detail"`
		} `json:"limits"`
	}
	if json.Unmarshal(body, &reply) != nil {
		return model.UnavailableUsage(model.ProviderKimiCode, model.ReasonUnreadableReply)
	}

	var windows []model.UsageWindow
	for idx, lim := range reply.Limits {
		if lim.Window == nil || lim.Window.Duration <= 0 {
			continue
		}
		unitSec := 0
		switch lim.Window.TimeUnit {
		case "TIME_UNIT_SECOND":
			unitSec = 1
		case "TIME_UNIT_MINUTE":
			unitSec = 60
		case "TIME_UNIT_HOUR":
			unitSec = 3600
		case "TIME_UNIT_DAY":
			unitSec = 86400
		}
		if unitSec == 0 {
			continue
		}
		secs := lim.Window.Duration * unitSec
		if w := parseKimiDetail(lim.Detail, fmt.Sprintf("limit.%d.%d", idx, secs), codexKind(secs), secs, true); w != nil {
			windows = append(windows, *w)
		}
	}
	if w := parseKimiDetail(reply.Usage, "weekly", model.KindWeekly, 7*86400, false); w != nil {
		windows = append(windows, *w)
	}
	if reply.Usages != nil {
		if w := parseKimiRatio(reply.Usages.MonthCode, "month.code", "Code"); w != nil {
			windows = append(windows, *w)
		}
		if w := parseKimiRatio(reply.Usages.MonthTotal, "month.total", "Total"); w != nil {
			windows = append(windows, *w)
		}
	}

	sort.SliceStable(windows, func(i, j int) bool {
		return windows[i].WindowSeconds < windows[j].WindowSeconds
	})

	if len(windows) == 0 {
		return model.UnavailableUsage(model.ProviderKimiCode, model.ReasonNoLimitsReported)
	}

	plan := ""
	if reply.User != nil && reply.User.Membership != nil {
		lvl := strings.TrimPrefix(reply.User.Membership.Level, "LEVEL_")
		if lvl != "" {
			plan = tidyTitle(strings.ToLower(lvl))
		}
	}

	now := time.Now()
	return model.ProviderUsage{
		Provider:   model.ProviderKimiCode,
		Windows:    windows,
		ObservedAt: &now,
		State:      model.StateLive,
		Plan:       plan,
		Source:     "endpoint",
	}
}

type kimiDetail struct {
	Limit     any    `json:"limit"`
	Used      any    `json:"used"`
	Remaining any    `json:"remaining"`
	ResetTime string `json:"resetTime"`
}

type kimiRatio struct {
	UsedRatio *float64 `json:"used_ratio"`
	ResetTime string   `json:"reset_time"`
}

func parseKimiDetail(d *kimiDetail, id string, kind model.WindowKind, secs int, reportsLength bool) *model.UsageWindow {
	if d == nil {
		return nil
	}
	limit, ok := toFloat(d.Limit)
	if !ok || limit <= 0 {
		return nil
	}
	used, okU := toFloat(d.Used)
	if !okU {
		rem, okR := toFloat(d.Remaining)
		if !okR {
			return nil
		}
		used = limit - rem
	}
	return &model.UsageWindow{
		ID:            id,
		Kind:          kind,
		UsedFraction:  clampFraction(used / limit),
		WindowSeconds: secs,
		ResetsAt:      parseISO8601(d.ResetTime),
		ReportsLength: reportsLength,
		IsExhausted:   used >= limit,
	}
}

func parseKimiRatio(r *kimiRatio, id, scope string) *model.UsageWindow {
	if r == nil || r.UsedRatio == nil {
		return nil
	}
	u := *r.UsedRatio
	return &model.UsageWindow{
		ID:            id,
		Kind:          model.KindMonthly,
		Scope:         scope,
		UsedFraction:  clampFraction(u),
		WindowSeconds: 30 * 86400,
		ResetsAt:      parseISO8601(r.ResetTime),
		ReportsLength: false,
		IsExhausted:   u >= 1,
	}
}
