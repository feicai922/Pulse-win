package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pulse-windows/internal/auth"
	"pulse-windows/internal/model"
)

func fetchCursor(ctx context.Context, proxyURL string) model.ProviderUsage {
	cookie, reason := auth.LoadCursorSessionCookie()
	if cookie == "" {
		if reason == "expired" || reason == "unreadable" {
			return model.UnavailableUsage(model.ProviderCursor, model.ReasonCursorLoginExpired)
		}
		return model.UnavailableUsage(model.ProviderCursor, model.ReasonCursorSignInRequired)
	}

	httpClient := NewHTTPClient(proxyURL, 15*time.Second, false)

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://cursor.com/api/usage-summary", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept", "application/json")

	status, body, err := doRequest(ctx, httpClient, req)
	if err != nil {
		return model.UnavailableUsage(model.ProviderCursor, model.ReasonUnreachable)
	}
	if status == 401 || status == 403 {
		return model.UnavailableUsage(model.ProviderCursor, model.ReasonCursorLoginExpired)
	}
	if status == 429 {
		return model.UnavailableUsage(model.ProviderCursor, model.ReasonRateLimited)
	}
	if status != 200 {
		return model.UnavailableUsage(model.ProviderCursor, model.ReasonServerError)
	}

	var reply cursorSummaryReply
	if json.Unmarshal(body, &reply) != nil {
		return model.UnavailableUsage(model.ProviderCursor, model.ReasonUnreadableReply)
	}

	windows := cursorWindowsFromSummary(reply)

	// Check if weekly plan applies
	pReq, _ := http.NewRequestWithContext(ctx, "POST", "https://cursor.com/api/dashboard/get-plan-info", bytes.NewReader([]byte("{}")))
	pReq.Header.Set("Cookie", cookie)
	pReq.Header.Set("Accept", "application/json")
	pReq.Header.Set("Content-Type", "application/json")
	pReq.Header.Set("Origin", "https://cursor.com")
	if st, pb, err := doRequest(ctx, httpClient, pReq); err == nil && st == 200 {
		var pInfo struct {
			PlanInfo *struct {
				IncludedUsagePeriod   any `json:"includedUsagePeriod"`
				IncludedUsageResetsAt any `json:"includedUsageResetsAt"`
			} `json:"planInfo"`
		}
		if json.Unmarshal(pb, &pInfo) == nil && pInfo.PlanInfo != nil && isCursorWeekly(pInfo.PlanInfo.IncludedUsagePeriod) {
			uReq, _ := http.NewRequestWithContext(ctx, "POST", "https://cursor.com/api/dashboard/get-current-period-usage", bytes.NewReader([]byte("{}")))
			uReq.Header.Set("Cookie", cookie)
			uReq.Header.Set("Accept", "application/json")
			uReq.Header.Set("Content-Type", "application/json")
			uReq.Header.Set("Origin", "https://cursor.com")
			ust, ub, uerr := doRequest(ctx, httpClient, uReq)
			if uerr != nil {
				return model.UnavailableUsage(model.ProviderCursor, model.ReasonUnreachable)
			}
			if ust == 401 || ust == 403 {
				return model.UnavailableUsage(model.ProviderCursor, model.ReasonCursorLoginExpired)
			}
			if ust != 200 {
				return model.UnavailableUsage(model.ProviderCursor, model.ReasonServerError)
			}
			var pu struct {
				PlanUsage *struct {
					TotalPercentUsed *float64 `json:"totalPercentUsed"`
				} `json:"planUsage"`
			}
			if json.Unmarshal(ub, &pu) == nil && pu.PlanUsage != nil && pu.PlanUsage.TotalPercentUsed != nil {
				pct := *pu.PlanUsage.TotalPercentUsed
				weekly := []model.UsageWindow{{
					ID:            "weekly",
					Kind:          model.KindWeekly,
					UsedFraction:  clampFraction(pct / 100),
					WindowSeconds: 7 * 86400,
					ResetsAt:      parseCursorMillis(pInfo.PlanInfo.IncludedUsageResetsAt),
					ReportsLength: true,
					IsExhausted:   pct >= 100,
				}}
				for _, w := range windows {
					if w.Kind == model.KindSpend {
						weekly = append(weekly, w)
					}
				}
				windows = weekly
			}
		}
	}

	if len(windows) == 0 {
		return model.UnavailableUsage(model.ProviderCursor, model.ReasonNoLimitsReported)
	}

	var credRem *model.CreditAmount
	var balStr string
	plan := reply.IndividualUsage.Plan
	if plan == nil {
		plan = reply.TeamUsage.Pooled
	}
	if plan == nil {
		plan = reply.IndividualUsage.Overall
	}
	if plan != nil && plan.Remaining != nil {
		amt := *plan.Remaining / 100
		credRem = &model.CreditAmount{Amount: amt, Currency: "USD"}
		balStr = credRem.FormatShort()
	}

	now := time.Now()
	return model.ProviderUsage{
		Provider:        model.ProviderCursor,
		Windows:         windows,
		ObservedAt:      &now,
		State:           model.StateLive,
		Plan:            cursorPlanName(reply.MembershipType),
		CreditBalance:   balStr,
		CreditRemaining: credRem,
		Source:          "endpoint",
	}
}

type cursorAllowance struct {
	Enabled         *bool    `json:"enabled"`
	Used            *float64 `json:"used"`
	Limit           *float64 `json:"limit"`
	Remaining       *float64 `json:"remaining"`
	AutoPercentUsed *float64 `json:"autoPercentUsed"`
	ApiPercentUsed  *float64 `json:"apiPercentUsed"`
}

type cursorSummaryReply struct {
	BillingCycleStart string `json:"billingCycleStart"`
	BillingCycleEnd   string `json:"billingCycleEnd"`
	MembershipType    string `json:"membershipType"`
	IndividualUsage   struct {
		Plan     *cursorAllowance `json:"plan"`
		OnDemand *cursorAllowance `json:"onDemand"`
		Overall  *cursorAllowance `json:"overall"`
	} `json:"individualUsage"`
	TeamUsage struct {
		Pooled   *cursorAllowance `json:"pooled"`
		OnDemand *cursorAllowance `json:"onDemand"`
	} `json:"teamUsage"`
}

func cursorWindowsFromSummary(r cursorSummaryReply) []model.UsageWindow {
	resets := parseISO8601(r.BillingCycleEnd)
	start := parseISO8601(r.BillingCycleStart)
	length := 0
	if start != nil && resets != nil {
		if d := int(resets.Sub(*start).Seconds() + 0.5); d >= 1 {
			length = d
		}
	}
	reportsLen := length > 0
	if length == 0 {
		length = 30 * 86400
	}

	plan := r.IndividualUsage.Plan
	if plan == nil {
		plan = r.TeamUsage.Pooled
	}
	var out []model.UsageWindow
	if plan != nil && plan.AutoPercentUsed != nil {
		p := *plan.AutoPercentUsed
		out = append(out, model.UsageWindow{
			ID: "cursorModels", Kind: model.KindMonthly, Scope: "Cursor Models",
			UsedFraction: clampFraction(p / 100), WindowSeconds: length, ResetsAt: resets,
			ReportsLength: reportsLen, IsExhausted: p >= 100,
		})
	}
	if plan != nil && plan.ApiPercentUsed != nil {
		p := *plan.ApiPercentUsed
		out = append(out, model.UsageWindow{
			ID: "otherModels", Kind: model.KindMonthly, Scope: "Other Models",
			UsedFraction: clampFraction(p / 100), WindowSeconds: length, ResetsAt: resets,
			ReportsLength: reportsLen, IsExhausted: p >= 100,
		})
	}
	if len(out) == 0 {
		src := plan
		if src == nil {
			src = r.IndividualUsage.Overall
		}
		if w := cursorMoneyWindow(src, "plan", model.KindMonthly, resets, length, reportsLen); w != nil {
			out = append(out, *w)
		}
	}
	od := r.IndividualUsage.OnDemand
	if od == nil {
		od = r.TeamUsage.OnDemand
	}
	if w := cursorMoneyWindow(od, "onDemand", model.KindSpend, resets, length, reportsLen); w != nil {
		out = append(out, *w)
	}
	return out
}

func cursorMoneyWindow(a *cursorAllowance, id string, kind model.WindowKind, resets *time.Time, length int, reportsLen bool) *model.UsageWindow {
	if a == nil || (a.Enabled != nil && !*a.Enabled) || a.Used == nil || a.Limit == nil || *a.Limit <= 0 {
		return nil
	}
	var remPtr *float64
	if a.Remaining != nil {
		v := *a.Remaining / 100
		remPtr = &v
	}
	return &model.UsageWindow{
		ID: id, Kind: kind,
		UsedFraction:  clampFraction(*a.Used / *a.Limit),
		WindowSeconds: length, ResetsAt: resets, ReportsLength: reportsLen,
		IsExhausted: *a.Used >= *a.Limit,
		Amounts: &model.UsageAmounts{
			Used: *a.Used / 100, Remaining: remPtr, Limit: *a.Limit / 100, Currency: "USD",
		},
	}
}

func isCursorWeekly(v any) bool {
	if n, ok := toFloat(v); ok {
		return int(n) == 2
	}
	if s, ok := v.(string); ok {
		u := strings.ToUpper(s)
		return u == "INCLUDED_USAGE_PERIOD_WEEKLY" || u == "WEEKLY"
	}
	return false
}

func parseCursorMillis(v any) *time.Time {
	if ms, ok := toFloat(v); ok && ms > 0 {
		t := time.UnixMilli(int64(ms))
		return &t
	}
	if s, ok := v.(string); ok {
		if ms, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && ms > 0 {
			t := time.UnixMilli(int64(ms))
			return &t
		}
	}
	return nil
}

func cursorPlanName(m string) string {
	m = strings.TrimSpace(m)
	if m == "" {
		return ""
	}
	parts := strings.Split(m, "_")
	var out strings.Builder
	for _, p := range parts {
		if p == "plus" {
			out.WriteString("+")
			continue
		}
		if out.Len() > 0 {
			out.WriteByte(' ')
		}
		if len(p) > 0 {
			out.WriteString(strings.ToUpper(p[:1]) + strings.ToLower(p[1:]))
		}
	}
	return out.String()
}
