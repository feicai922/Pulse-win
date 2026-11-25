package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"pulse-windows/internal/model"
)

// --- Command Code ---

func fetchCommandCode(ctx context.Context, client *http.Client, enteredKey string) model.ProviderUsage {
	key := strings.TrimSpace(enteredKey)
	if key == "" {
		home, _ := os.UserHomeDir()
		if raw, err := os.ReadFile(filepath.Join(home, ".commandcode", "auth.json")); err == nil {
			var doc map[string]any
			if json.Unmarshal(raw, &doc) == nil {
				if k, ok := doc["apiKey"].(string); ok {
					key = strings.TrimSpace(k)
				}
			}
		}
	}
	if key == "" {
		return model.UnavailableUsage(model.ProviderCommandCode, model.ReasonAPIKeyMissing)
	}

	getCC := func(path string, query url.Values) (int, []byte, error) {
		u := url.URL{Scheme: "https", Host: "api.commandcode.ai", Path: path, RawQuery: query.Encode()}
		req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		return doRequest(ctx, client, req)
	}

	wq := url.Values{}
	wq.Set("limits", "1")
	st, wb, err := getCC("/alpha/whoami", wq)
	if err != nil {
		return model.UnavailableUsage(model.ProviderCommandCode, model.ReasonUnreachable)
	}
	if st == 401 || st == 403 {
		return model.UnavailableUsage(model.ProviderCommandCode, model.ReasonAPIKeyRefused)
	}
	if st == 429 {
		return model.UnavailableUsage(model.ProviderCommandCode, model.ReasonRateLimited)
	}
	if st != 200 {
		return model.UnavailableUsage(model.ProviderCommandCode, model.ReasonServerError)
	}
	var whoami struct {
		Org *struct {
			ID string `json:"id"`
		} `json:"org"`
		OrgLimits []struct {
			Scope         string   `json:"scope"`
			ModelLabel    string   `json:"modelLabel"`
			Model         string   `json:"model"`
			Spent         *float64 `json:"spent"`
			Limit         *float64 `json:"limit"`
			Exceeded      bool     `json:"exceeded"`
			ResetInterval string   `json:"resetInterval"`
			ResetAt       string   `json:"resetAt"`
		} `json:"orgLimits"`
	}
	if json.Unmarshal(wb, &whoami) != nil {
		return model.UnavailableUsage(model.ProviderCommandCode, model.ReasonUnreadableReply)
	}

	oq := url.Values{}
	if whoami.Org != nil && whoami.Org.ID != "" {
		oq.Set("orgId", whoami.Org.ID)
	}
	cst, cb, err := getCC("/alpha/billing/credits", oq)
	if err != nil {
		return model.UnavailableUsage(model.ProviderCommandCode, model.ReasonUnreachable)
	}
	if cst == 401 || cst == 403 {
		return model.UnavailableUsage(model.ProviderCommandCode, model.ReasonAPIKeyRefused)
	}
	if cst != 200 {
		return model.UnavailableUsage(model.ProviderCommandCode, model.ReasonServerError)
	}
	var credits struct {
		Credits *struct {
			PlanID           string   `json:"planId"`
			MonthlyCredits   *float64 `json:"monthlyCredits"`
			PurchasedCredits *float64 `json:"purchasedCredits"`
			FreeCredits      *float64 `json:"freeCredits"`
		} `json:"credits"`
		WindowLimits *struct {
			Limited  bool `json:"limited"`
			FiveHour *struct {
				Used    *float64 `json:"used"`
				Cap     *float64 `json:"cap"`
				ResetAt *float64 `json:"resetAt"`
			} `json:"fiveHour"`
			Weekly *struct {
				Used    *float64 `json:"used"`
				Cap     *float64 `json:"cap"`
				ResetAt *float64 `json:"resetAt"`
			} `json:"weekly"`
		} `json:"windowLimits"`
	}
	if json.Unmarshal(cb, &credits) != nil {
		return model.UnavailableUsage(model.ProviderCommandCode, model.ReasonUnreadableReply)
	}

	var windows []model.UsageWindow
	if credits.WindowLimits != nil && credits.WindowLimits.Limited {
		if fh := credits.WindowLimits.FiveHour; fh != nil && fh.Used != nil && fh.Cap != nil && *fh.Cap > 0 {
			var res *time.Time
			if fh.ResetAt != nil && *fh.ResetAt > 0 {
				t := time.UnixMilli(int64(*fh.ResetAt))
				res = &t
			}
			windows = append(windows, model.UsageWindow{
				ID: "commandCode.fiveHour", Kind: model.KindFiveHour,
				UsedFraction: clampFraction(*fh.Used / *fh.Cap), WindowSeconds: 5 * 3600,
				ResetsAt: res, ReportsLength: true, IsExhausted: *fh.Used >= *fh.Cap,
			})
		}
		if wk := credits.WindowLimits.Weekly; wk != nil && wk.Used != nil && wk.Cap != nil && *wk.Cap > 0 {
			var res *time.Time
			if wk.ResetAt != nil && *wk.ResetAt > 0 {
				t := time.UnixMilli(int64(*wk.ResetAt))
				res = &t
			}
			windows = append(windows, model.UsageWindow{
				ID: "commandCode.weekly", Kind: model.KindWeekly,
				UsedFraction: clampFraction(*wk.Used / *wk.Cap), WindowSeconds: 7 * 86400,
				ResetsAt: res, ReportsLength: true, IsExhausted: *wk.Used >= *wk.Cap,
			})
		}
	}
	for idx, ol := range whoami.OrgLimits {
		if ol.Spent == nil || ol.Limit == nil || *ol.Limit <= 0 {
			continue
		}
		kind := model.KindSpend
		secs := 30 * 86400
		switch strings.ToLower(ol.ResetInterval) {
		case "daily":
			kind = model.KindDaily
			secs = 86400
		case "weekly":
			kind = model.KindWeekly
			secs = 7 * 86400
		case "monthly":
			kind = model.KindMonthly
		}
		scope := ol.ModelLabel
		if scope == "" && ol.Scope == "model" {
			scope = ol.Model
		}
		windows = append(windows, model.UsageWindow{
			ID: fmt.Sprintf("commandCode.org.%d", idx), Kind: kind, Scope: scope,
			UsedFraction: clampFraction(*ol.Spent / *ol.Limit), WindowSeconds: secs,
			ResetsAt: parseISO8601(ol.ResetAt), ReportsLength: false,
			IsExhausted: ol.Exceeded || *ol.Spent >= *ol.Limit,
		})
	}

	var credRem *model.CreditAmount
	var balStr string
	var plan string
	if credits.Credits != nil {
		plan = tidyTitle(credits.Credits.PlanID)
		total := 0.0
		hasAny := false
		for _, v := range []*float64{credits.Credits.MonthlyCredits, credits.Credits.PurchasedCredits, credits.Credits.FreeCredits} {
			if v != nil {
				total += *v
				hasAny = true
			}
		}
		if hasAny {
			credRem = &model.CreditAmount{Amount: total, Currency: "USD"}
			balStr = credRem.FormatShort()
		}
	}

	if len(windows) == 0 && credRem == nil {
		return model.UnavailableUsage(model.ProviderCommandCode, model.ReasonNoLimitsReported)
	}
	now := time.Now()
	return model.ProviderUsage{
		Provider: model.ProviderCommandCode, Windows: windows, ObservedAt: &now,
		State: model.StateLive, Plan: plan, CreditBalance: balStr, CreditRemaining: credRem, Source: "endpoint",
	}
}

// --- Grok ---

func fetchGrok(ctx context.Context, client *http.Client) model.ProviderUsage {
	home, _ := os.UserHomeDir()
	raw, err := os.ReadFile(filepath.Join(home, ".grok", "auth.json"))
	if err != nil {
		return model.UnavailableUsage(model.ProviderGrok, model.ReasonGrokSignInRequired)
	}
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil {
		return model.UnavailableUsage(model.ProviderGrok, model.ReasonGrokSignInRequired)
	}
	var bestToken string
	var bestExp time.Time
	sawEntry := false
	for _, v := range root {
		m, _ := v.(map[string]any)
		if m == nil {
			continue
		}
		tok, _ := m["key"].(string)
		if tok == "" {
			continue
		}
		sawEntry = true
		expStr, _ := m["expires_at"].(string)
		exp := parseISO8601(expStr)
		if exp != nil && exp.Before(time.Now()) {
			continue
		}
		if bestToken == "" || (exp != nil && exp.After(bestExp)) {
			bestToken = tok
			if exp != nil {
				bestExp = *exp
			}
		}
	}
	if bestToken == "" {
		if sawEntry {
			return model.UnavailableUsage(model.ProviderGrok, model.ReasonGrokLoginExpired)
		}
		return model.UnavailableUsage(model.ProviderGrok, model.ReasonGrokSignInRequired)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://cli-chat-proxy.grok.com/v1/billing?format=credits", nil)
	req.Header.Set("Authorization", "Bearer "+bestToken)
	req.Header.Set("x-xai-token-auth", "xai-grok-cli")
	req.Header.Set("Accept", "application/json")

	status, body, err := doRequest(ctx, client, req)
	if err != nil {
		return model.UnavailableUsage(model.ProviderGrok, model.ReasonUnreachable)
	}
	if status == 401 || status == 403 {
		return model.UnavailableUsage(model.ProviderGrok, model.ReasonGrokLoginExpired)
	}
	if status == 429 {
		return model.UnavailableUsage(model.ProviderGrok, model.ReasonRateLimited)
	}
	if status != 200 {
		return model.UnavailableUsage(model.ProviderGrok, model.ReasonServerError)
	}
	var reply struct {
		Config *struct {
			CurrentPeriod *struct {
				Start string `json:"start"`
				End   string `json:"end"`
			} `json:"currentPeriod"`
			CreditUsagePercent *float64 `json:"creditUsagePercent"`
			BillingPeriodStart string   `json:"billingPeriodStart"`
			BillingPeriodEnd   string   `json:"billingPeriodEnd"`
		} `json:"config"`
	}
	if json.Unmarshal(body, &reply) != nil || reply.Config == nil {
		return model.UnavailableUsage(model.ProviderGrok, model.ReasonUnreadableReply)
	}
	startStr, endStr := reply.Config.BillingPeriodStart, reply.Config.BillingPeriodEnd
	if reply.Config.CurrentPeriod != nil {
		if reply.Config.CurrentPeriod.Start != "" {
			startStr = reply.Config.CurrentPeriod.Start
		}
		if reply.Config.CurrentPeriod.End != "" {
			endStr = reply.Config.CurrentPeriod.End
		}
	}
	start, end := parseISO8601(startStr), parseISO8601(endStr)
	if start == nil || end == nil || !end.After(*start) {
		return model.UnavailableUsage(model.ProviderGrok, model.ReasonNoLimitsReported)
	}
	secs := int(end.Sub(*start).Seconds() + 0.5)
	pct := 0.0
	if reply.Config.CreditUsagePercent != nil {
		pct = *reply.Config.CreditUsagePercent
	} else if time.Now().After(*start) && time.Now().Before(*end) {
		pct = 0
	} else {
		return model.UnavailableUsage(model.ProviderGrok, model.ReasonNoLimitsReported)
	}
	kind := model.KindOther
	if secs >= 6*86400 && secs <= 8*86400 {
		kind = model.KindWeekly
	} else if secs >= 27*86400 && secs <= 32*86400 {
		kind = model.KindMonthly
	}
	now := time.Now()
	return model.ProviderUsage{
		Provider: model.ProviderGrok,
		Windows: []model.UsageWindow{{
			ID: "grok-pool", Kind: kind, UsedFraction: clampFraction(pct / 100),
			WindowSeconds: secs, ResetsAt: end, ReportsLength: true, IsExhausted: pct >= 100,
		}},
		ObservedAt: &now, State: model.StateLive, Source: "endpoint",
	}
}

// --- Volcengine (arkcli) ---

func fetchVolcengine(ctx context.Context) model.ProviderUsage {
	bin, err := exec.LookPath("arkcli")
	if err != nil {
		return model.UnavailableUsage(model.ProviderVolcengine, model.ReasonLocalLoginMissing)
	}
	cctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, bin, "usage", "plan", "--format", "json").Output()
	if err != nil {
		return model.UnavailableUsage(model.ProviderVolcengine, model.ReasonUnreachable)
	}
	var root map[string]any
	if json.Unmarshal(out, &root) != nil {
		return model.UnavailableUsage(model.ProviderVolcengine, model.ReasonUnreadableReply)
	}
	var windows []model.UsageWindow
	plans, _ := root["plans"].([]any)
	for pi, item := range plans {
		pm, _ := item.(map[string]any)
		if pm == nil {
			continue
		}
		pname, _ := pm["name"].(string)
		wins, _ := pm["windows"].([]any)
		for wi, witem := range wins {
			wm, _ := witem.(map[string]any)
			if wm == nil {
				continue
			}
			pct, ok := toFloat(wm["used_percent"])
			if !ok {
				pct, ok = toFloat(wm["usedPercent"])
			}
			if !ok {
				continue
			}
			secsF, _ := toFloat(wm["window_seconds"])
			secs := int(secsF)
			resStr, _ := wm["resets_at"].(string)
			windows = append(windows, model.UsageWindow{
				ID: fmt.Sprintf("volc.%d.%d", pi, wi), Kind: codexKind(secs), Scope: pname,
				UsedFraction: clampFraction(pct / 100), WindowSeconds: secs,
				ResetsAt: parseISO8601(resStr), ReportsLength: secs > 0, IsExhausted: pct >= 100,
			})
		}
	}
	if len(windows) == 0 {
		return model.UnavailableUsage(model.ProviderVolcengine, model.ReasonNoLimitsReported)
	}
	now := time.Now()
	return model.ProviderUsage{
		Provider: model.ProviderVolcengine, Windows: windows, ObservedAt: &now, State: model.StateLive, Source: "arkCLI",
	}
}

// --- Alibaba Coding Plan ---

func fetchAlibabaCodingPlan(ctx context.Context, client *http.Client, apiKey string) model.ProviderUsage {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return model.UnavailableUsage(model.ProviderAlibabaCodingPlan, model.ReasonAPIKeyMissing)
	}
	sites := []struct {
		origin, region, commodity, page string
	}{
		{"https://modelstudio.console.alibabacloud.com", "ap-southeast-1", "sfm_codingplan_public_intl", "https://modelstudio.console.alibabacloud.com/ap-southeast-1/?tab=coding-plan#/efm/coding_plan"},
		{"https://bailian.console.aliyun.com", "cn-beijing", "sfm_codingplan_public_cn", "https://bailian.console.aliyun.com/cn-beijing/?tab=model#/efm/coding_plan"},
	}

	var firstReason model.Unavailability
	for _, s := range sites {
		ep := fmt.Sprintf("%s/data/api.json?action=zeldaEasy.broadscope-bailian.codingPlan.queryCodingPlanInstanceInfoV2&product=broadscope-bailian&api=queryCodingPlanInstanceInfoV2&currentRegionId=%s", s.origin, s.region)
		payload, _ := json.Marshal(map[string]any{"queryCodingPlanInstanceInfoRequest": map[string]string{"commodityCode": s.commodity}})
		req, _ := http.NewRequestWithContext(ctx, "POST", ep, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("x-api-key", key)
		req.Header.Set("X-DashScope-API-Key", key)
		req.Header.Set("Origin", s.origin)
		req.Header.Set("Referer", s.page)

		st, body, err := doRequest(ctx, client, req)
		if err != nil {
			return model.UnavailableUsage(model.ProviderAlibabaCodingPlan, model.ReasonUnreachable)
		}
		if st == 401 || st == 403 {
			if firstReason == "" {
				firstReason = model.ReasonAPIKeyRefused
			}
			continue
		}
		if st == 404 {
			if firstReason == "" {
				firstReason = model.ReasonServerError
			}
			continue
		}
		if st == 429 {
			return model.UnavailableUsage(model.ProviderAlibabaCodingPlan, model.ReasonRateLimited)
		}
		if st != 200 {
			return model.UnavailableUsage(model.ProviderAlibabaCodingPlan, model.ReasonServerError)
		}

		var root map[string]any
		if json.Unmarshal(body, &root) != nil {
			return model.UnavailableUsage(model.ProviderAlibabaCodingPlan, model.ReasonUnreadableReply)
		}
		quota := findMapRecursive(root, "codingPlanQuotaInfo", "coding_plan_quota_info")
		if quota == nil {
			if firstReason == "" {
				firstReason = model.ReasonNoLimitsReported
			}
			continue
		}
		windows := parseAlibabaQuota(quota)
		if len(windows) > 0 {
			now := time.Now()
			return model.ProviderUsage{
				Provider: model.ProviderAlibabaCodingPlan, Windows: windows, ObservedAt: &now, State: model.StateLive, Source: "endpoint",
			}
		}
		if firstReason == "" {
			firstReason = model.ReasonNoLimitsReported
		}
	}
	if firstReason == "" {
		firstReason = model.ReasonAPIKeyRefused
	}
	return model.UnavailableUsage(model.ProviderAlibabaCodingPlan, firstReason)
}

func parseAlibabaQuota(q map[string]any) []model.UsageWindow {
	specs := []struct {
		id     string
		kind   model.WindowKind
		secs   int
		repLen bool
		usedK  []string
		totK   []string
		resK   []string
	}{
		{"fiveHour", model.KindFiveHour, 5 * 3600, true, []string{"per5HourUsedQuota", "perFiveHourUsedQuota"}, []string{"per5HourTotalQuota", "perFiveHourTotalQuota"}, []string{"per5HourQuotaNextRefreshTime", "perFiveHourQuotaNextRefreshTime"}},
		{"weekly", model.KindWeekly, 7 * 86400, true, []string{"perWeekUsedQuota"}, []string{"perWeekTotalQuota"}, []string{"perWeekQuotaNextRefreshTime"}},
		{"monthly", model.KindMonthly, 30 * 86400, false, []string{"perBillMonthUsedQuota", "perMonthUsedQuota"}, []string{"perBillMonthTotalQuota", "perMonthTotalQuota"}, []string{"perBillMonthQuotaNextRefreshTime", "perMonthQuotaNextRefreshTime"}},
	}
	var out []model.UsageWindow
	for _, sp := range specs {
		var used, total float64
		var hasU, hasT bool
		for _, k := range sp.usedK {
			if v, ok := toFloat(q[k]); ok {
				used, hasU = v, true
				break
			}
		}
		for _, k := range sp.totK {
			if v, ok := toFloat(q[k]); ok {
				total, hasT = v, true
				break
			}
		}
		if !hasU || !hasT || total <= 0 || used < 0 {
			continue
		}
		var resets *time.Time
		for _, k := range sp.resK {
			if s, ok := q[k].(string); ok {
				if t := parseISO8601(s); t != nil && t.After(time.Now()) {
					resets = t
					break
				}
			} else if ms, ok := toFloat(q[k]); ok && ms > 0 {
				t := time.UnixMilli(int64(ms))
				if t.After(time.Now()) {
					resets = &t
					break
				}
			}
		}
		out = append(out, model.UsageWindow{
			ID: "alibabaCodingPlan." + sp.id, Kind: sp.kind,
			UsedFraction: clampFraction(used / total), WindowSeconds: sp.secs,
			ResetsAt: resets, ReportsLength: sp.repLen, IsExhausted: used >= total,
		})
	}
	return out
}

func findMapRecursive(v any, keys ...string) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range keys {
			if sub, ok := x[k].(map[string]any); ok {
				return sub
			}
		}
		for _, child := range x {
			if found := findMapRecursive(child, keys...); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range x {
			if found := findMapRecursive(child, keys...); found != nil {
				return found
			}
		}
	}
	return nil
}

// --- Moonshot ---

func fetchMoonshot(ctx context.Context, client *http.Client, apiKey string, basis model.BalanceBasis, budget float64) model.ProviderUsage {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return model.UnavailableUsage(model.ProviderMoonshot, model.ReasonAPIKeyMissing)
	}
	regions := []struct{ ep, cur string }{
		{"https://api.moonshot.ai/v1/users/me/balance", "USD"},
		{"https://api.moonshot.cn/v1/users/me/balance", "CNY"},
	}
	for _, r := range regions {
		req, _ := http.NewRequestWithContext(ctx, "GET", r.ep, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Accept", "application/json")
		st, body, err := doRequest(ctx, client, req)
		if err != nil {
			return model.UnavailableUsage(model.ProviderMoonshot, model.ReasonUnreachable)
		}
		if st == 401 || st == 403 {
			continue
		}
		if st == 429 {
			return model.UnavailableUsage(model.ProviderMoonshot, model.ReasonRateLimited)
		}
		if st != 200 {
			return model.UnavailableUsage(model.ProviderMoonshot, model.ReasonServerError)
		}
		var reply struct {
			Data *struct {
				AvailableBalance *float64 `json:"available_balance"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &reply) != nil || reply.Data == nil || reply.Data.AvailableBalance == nil {
			return model.UnavailableUsage(model.ProviderMoonshot, model.ReasonUnreadableReply)
		}
		bal := *reply.Data.AvailableBalance
		cred := &model.CreditAmount{Amount: bal, Currency: r.cur}
		now := time.Now()
		u := model.ProviderUsage{
			Provider: model.ProviderMoonshot, ObservedAt: &now, State: model.StateLive,
			CreditBalance: cred.FormatShort(), CreditRemaining: cred, Source: "endpoint",
		}
		return ApplyBalanceRing(u, basis, budget)
	}
	return model.UnavailableUsage(model.ProviderMoonshot, model.ReasonAPIKeyRefused)
}

// --- OpenAI API ---

func fetchOpenAIPlatform(ctx context.Context, client *http.Client, apiKey string, basis model.BalanceBasis, budget float64) model.ProviderUsage {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return model.UnavailableUsage(model.ProviderOpenAIPlatform, model.ReasonAPIKeyMissing)
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.openai.com/v1/dashboard/billing/credit_grants", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	st, body, err := doRequest(ctx, client, req)
	if err != nil {
		return model.UnavailableUsage(model.ProviderOpenAIPlatform, model.ReasonUnreachable)
	}
	if st == 401 || st == 403 {
		return model.UnavailableUsage(model.ProviderOpenAIPlatform, model.ReasonAPIKeyRefused)
	}
	if st == 429 {
		return model.UnavailableUsage(model.ProviderOpenAIPlatform, model.ReasonRateLimited)
	}
	if st != 200 {
		// Probe admin costs endpoint before calling key bad
		probeURL := fmt.Sprintf("https://api.openai.com/v1/organization/costs?start_time=%d&limit=1", time.Now().Unix()-86400)
		preq, _ := http.NewRequestWithContext(ctx, "GET", probeURL, nil)
		preq.Header.Set("Authorization", "Bearer "+key)
		pst, _, _ := doRequest(ctx, client, preq)
		if pst == 200 {
			return model.UnavailableUsage(model.ProviderOpenAIPlatform, model.ReasonNoLimitsReported)
		}
		return model.UnavailableUsage(model.ProviderOpenAIPlatform, model.ReasonAPIKeyRefused)
	}
	var reply struct {
		TotalAvailable *float64 `json:"total_available"`
	}
	if json.Unmarshal(body, &reply) != nil {
		return model.UnavailableUsage(model.ProviderOpenAIPlatform, model.ReasonUnreadableReply)
	}
	if reply.TotalAvailable == nil || *reply.TotalAvailable < 0 {
		return model.UnavailableUsage(model.ProviderOpenAIPlatform, model.ReasonNoLimitsReported)
	}
	cred := &model.CreditAmount{Amount: *reply.TotalAvailable, Currency: "USD"}
	now := time.Now()
	u := model.ProviderUsage{
		Provider: model.ProviderOpenAIPlatform, ObservedAt: &now, State: model.StateLive,
		CreditBalance: cred.FormatShort(), CreditRemaining: cred, Source: "endpoint",
	}
	return ApplyBalanceRing(u, basis, budget)
}

// --- Kilo Code ---

func fetchKiloCode(ctx context.Context, client *http.Client, enteredKey string, basis model.BalanceBasis, budget float64) model.ProviderUsage {
	key := strings.TrimSpace(enteredKey)
	refused := model.ReasonAPIKeyRefused
	if key == "" {
		home, _ := os.UserHomeDir()
		raw, err := os.ReadFile(filepath.Join(home, ".local", "share", "kilo", "auth.json"))
		if err != nil {
			return model.UnavailableUsage(model.ProviderKiloCode, model.ReasonAPIKeyMissing)
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) == nil {
			if k, ok := doc["kilo"].(map[string]any); ok {
				if acc, ok := k["access"].(string); ok {
					key = strings.TrimSpace(acc)
				}
			}
		}
		if key == "" {
			return model.UnavailableUsage(model.ProviderKiloCode, model.ReasonLocalLoginMissing)
		}
		refused = model.ReasonLocalLoginExpired
	}
	ep := `https://app.kilo.ai/api/trpc/user.getCreditBlocks,kiloPass.getState?batch=1&input=%7B%220%22%3A%7B%22json%22%3Anull%7D%2C%221%22%3A%7B%22json%22%3Anull%7D%7D`
	req, _ := http.NewRequestWithContext(ctx, "GET", ep, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	st, body, err := doRequest(ctx, client, req)
	if err != nil {
		return model.UnavailableUsage(model.ProviderKiloCode, model.ReasonUnreachable)
	}
	if st == 401 || st == 403 {
		return model.UnavailableUsage(model.ProviderKiloCode, refused)
	}
	if st == 429 {
		return model.UnavailableUsage(model.ProviderKiloCode, model.ReasonRateLimited)
	}
	if st != 200 {
		return model.UnavailableUsage(model.ProviderKiloCode, model.ReasonServerError)
	}
	var batch []struct {
		Result *struct {
			Data *struct {
				JSON map[string]any `json:"json"`
			} `json:"data"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &batch) != nil || len(batch) == 0 {
		return model.UnavailableUsage(model.ProviderKiloCode, model.ReasonUnreadableReply)
	}
	var credRem *model.CreditAmount
	var balStr string
	if batch[0].Result != nil && batch[0].Result.Data != nil {
		if total, ok := toFloat(batch[0].Result.Data.JSON["totalBalance"]); ok {
			credRem = &model.CreditAmount{Amount: total, Currency: "USD"}
			balStr = credRem.FormatShort()
		}
	}
	var windows []model.UsageWindow
	var plan string
	if len(batch) > 1 && batch[1].Result != nil && batch[1].Result.Data != nil {
		j := batch[1].Result.Data.JSON
		if p, ok := j["tier"].(string); ok {
			plan = p
		}
		if used, okU := toFloat(j["used"]); okU {
			if limit, okL := toFloat(j["limit"]); okL && limit > 0 {
				resStr, _ := j["resetsAt"].(string)
				windows = append(windows, model.UsageWindow{
					ID: "kiloPass", Kind: model.KindMonthly, UsedFraction: clampFraction(used / limit),
					WindowSeconds: 30 * 86400, ResetsAt: parseISO8601(resStr), ReportsLength: false, IsExhausted: used >= limit,
				})
			}
		}
	}
	if len(windows) == 0 && credRem == nil {
		return model.UnavailableUsage(model.ProviderKiloCode, model.ReasonNoLimitsReported)
	}
	now := time.Now()
	u := model.ProviderUsage{
		Provider: model.ProviderKiloCode, Windows: windows, ObservedAt: &now, State: model.StateLive,
		Plan: plan, CreditBalance: balStr, CreditRemaining: credRem, Source: "endpoint",
	}
	return ApplyBalanceRing(u, basis, budget)
}

// --- sub2api & New API ---

func validateGatewayURL(raw, suffixPath string, trimSuffixes []string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" {
		if scheme != "http" || !isPrivateHost(u.Hostname()) {
			return "", false
		}
	}
	p := strings.TrimRight(u.Path, "/")
	for _, suf := range trimSuffixes {
		if strings.HasSuffix(p, suf) {
			p = strings.TrimSuffix(p, suf)
			break
		}
	}
	u.Path = p + suffixPath
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), true
}

func isPrivateHost(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".local") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func fetchSub2API(ctx context.Context, client *http.Client, apiKey, address string, basis model.BalanceBasis, budget float64) model.ProviderUsage {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return model.UnavailableUsage(model.ProviderSub2API, model.ReasonAPIKeyMissing)
	}
	if strings.TrimSpace(address) == "" {
		return model.UnavailableUsage(model.ProviderSub2API, model.ReasonServerAddressMissing)
	}
	ep, ok := validateGatewayURL(address, "/v1/usage", []string{"/v1/usage", "/v1"})
	if !ok {
		return model.UnavailableUsage(model.ProviderSub2API, model.ReasonServerAddressRefused)
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", ep, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	st, body, err := doRequest(ctx, client, req)
	if err != nil {
		return model.UnavailableUsage(model.ProviderSub2API, model.ReasonUnreachable)
	}
	if st == 401 || st == 403 {
		return model.UnavailableUsage(model.ProviderSub2API, model.ReasonAPIKeyRefused)
	}
	if st == 429 {
		return model.UnavailableUsage(model.ProviderSub2API, model.ReasonRateLimited)
	}
	if st != 200 {
		return model.UnavailableUsage(model.ProviderSub2API, model.ReasonServerError)
	}
	var reply struct {
		IsValid      *bool    `json:"isValid"`
		PlanName     string   `json:"planName"`
		Balance      *float64 `json:"balance"`
		Remaining    *float64 `json:"remaining"`
		Unit         string   `json:"unit"`
		Quota        *struct {
			Limit     *float64 `json:"limit"`
			Used      *float64 `json:"used"`
			Remaining *float64 `json:"remaining"`
		} `json:"quota"`
		Subscription *struct {
			DailyUsage   *float64 `json:"daily_usage_usd"`
			WeeklyUsage  *float64 `json:"weekly_usage_usd"`
			MonthlyUsage *float64 `json:"monthly_usage_usd"`
			DailyLimit   *float64 `json:"daily_limit_usd"`
			WeeklyLimit  *float64 `json:"weekly_limit_usd"`
			MonthlyLimit *float64 `json:"monthly_limit_usd"`
		} `json:"subscription"`
		RateLimits []struct {
			Window    string   `json:"window"`
			Limit     *float64 `json:"limit"`
			Used      *float64 `json:"used"`
			Remaining *float64 `json:"remaining"`
			ResetAt   string   `json:"reset_at"`
		} `json:"rate_limits"`
	}
	if json.Unmarshal(body, &reply) != nil {
		return model.UnavailableUsage(model.ProviderSub2API, model.ReasonUnreadableReply)
	}
	if reply.IsValid != nil && !*reply.IsValid {
		return model.UnavailableUsage(model.ProviderSub2API, model.ReasonAPIKeyRefused)
	}

	var windows []model.UsageWindow
	for _, rl := range reply.RateLimits {
		if rl.Limit == nil || *rl.Limit <= 0 || rl.Used == nil || len(rl.Window) < 2 {
			continue
		}
		unit := rl.Window[len(rl.Window)-1]
		cnt, err := strconv.Atoi(rl.Window[:len(rl.Window)-1])
		if err != nil || cnt <= 0 {
			continue
		}
		secs := 0
		if unit == 'h' || unit == 'H' {
			secs = cnt * 3600
		} else if unit == 'd' || unit == 'D' {
			secs = cnt * 86400
		} else {
			continue
		}
		exh := rl.Remaining != nil && *rl.Remaining <= 0
		windows = append(windows, model.UsageWindow{
			ID: "rate." + strings.ToLower(rl.Window), Kind: codexKind(secs),
			UsedFraction: clampFraction(*rl.Used / *rl.Limit), WindowSeconds: secs,
			ResetsAt: parseISO8601(rl.ResetAt), ReportsLength: true, IsExhausted: exh,
		})
	}
	if reply.Quota != nil && reply.Quota.Limit != nil && *reply.Quota.Limit > 0 && reply.Quota.Used != nil {
		exh := reply.Quota.Remaining != nil && *reply.Quota.Remaining <= 0
		windows = append(windows, model.UsageWindow{
			ID: "quota", Kind: model.KindSpend,
			UsedFraction: clampFraction(*reply.Quota.Used / *reply.Quota.Limit), WindowSeconds: 30 * 86400,
			ReportsLength: false, IsExhausted: exh,
		})
	}
	if sub := reply.Subscription; sub != nil {
		addSub := func(id string, kind model.WindowKind, secs int, u, l *float64) {
			if u != nil && l != nil && *l > 0 {
				windows = append(windows, model.UsageWindow{
					ID: "subscription." + id, Kind: kind,
					UsedFraction: clampFraction(*u / *l), WindowSeconds: secs, ReportsLength: false,
				})
			}
		}
		addSub("daily", model.KindDaily, 86400, sub.DailyUsage, sub.DailyLimit)
		addSub("weekly", model.KindWeekly, 7*86400, sub.WeeklyUsage, sub.WeeklyLimit)
		addSub("monthly", model.KindMonthly, 30*86400, sub.MonthlyUsage, sub.MonthlyLimit)
	}
	sort.SliceStable(windows, func(i, j int) bool { return windows[i].WindowSeconds < windows[j].WindowSeconds })

	var credRem *model.CreditAmount
	var balStr string
	remVal := reply.Remaining
	if remVal == nil {
		remVal = reply.Balance
	}
	if remVal != nil {
		cur := reply.Unit
		if cur == "" {
			cur = "USD"
		}
		credRem = &model.CreditAmount{Amount: *remVal, Currency: cur}
		balStr = credRem.FormatShort()
	}
	if len(windows) == 0 && credRem == nil {
		return model.UnavailableUsage(model.ProviderSub2API, model.ReasonNoLimitsReported)
	}
	now := time.Now()
	u := model.ProviderUsage{
		Provider: model.ProviderSub2API, Windows: windows, ObservedAt: &now, State: model.StateLive,
		Plan: strings.TrimSpace(reply.PlanName), CreditBalance: balStr, CreditRemaining: credRem, Source: "endpoint",
	}
	return ApplyBalanceRing(u, basis, budget)
}

func fetchNewAPI(ctx context.Context, client *http.Client, apiKey, address string, basis model.BalanceBasis, budget float64) model.ProviderUsage {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return model.UnavailableUsage(model.ProviderNewAPI, model.ReasonAPIKeyMissing)
	}
	if strings.TrimSpace(address) == "" {
		return model.UnavailableUsage(model.ProviderNewAPI, model.ReasonServerAddressMissing)
	}
	subURL, ok1 := validateGatewayURL(address, "/v1/dashboard/billing/subscription", []string{"/v1"})
	useURL, ok2 := validateGatewayURL(address, "/v1/dashboard/billing/usage", []string{"/v1"})
	stURL, ok3 := validateGatewayURL(address, "/api/status", []string{"/v1"})
	if !ok1 || !ok2 || !ok3 {
		return model.UnavailableUsage(model.ProviderNewAPI, model.ReasonServerAddressRefused)
	}

	reqSub, _ := http.NewRequestWithContext(ctx, "GET", subURL, nil)
	reqSub.Header.Set("Authorization", "Bearer "+key)
	st1, b1, err1 := doRequest(ctx, client, reqSub)
	if err1 != nil {
		return model.UnavailableUsage(model.ProviderNewAPI, model.ReasonUnreachable)
	}
	if st1 == 401 || st1 == 403 {
		return model.UnavailableUsage(model.ProviderNewAPI, model.ReasonAPIKeyRefused)
	}
	if st1 != 200 {
		return model.UnavailableUsage(model.ProviderNewAPI, model.ReasonServerError)
	}
	var sub struct {
		HardLimitUSD *float64 `json:"hard_limit_usd"`
	}
	if json.Unmarshal(b1, &sub) != nil || sub.HardLimitUSD == nil {
		return model.UnavailableUsage(model.ProviderNewAPI, model.ReasonUnreadableReply)
	}
	if *sub.HardLimitUSD >= 100_000_000 {
		return model.UnavailableUsage(model.ProviderNewAPI, model.ReasonNoLimitsReported)
	}

	reqUse, _ := http.NewRequestWithContext(ctx, "GET", useURL, nil)
	reqUse.Header.Set("Authorization", "Bearer "+key)
	st2, b2, err2 := doRequest(ctx, client, reqUse)
	if err2 != nil || st2 != 200 {
		return model.UnavailableUsage(model.ProviderNewAPI, model.ReasonServerError)
	}
	var use struct {
		TotalUsage *float64 `json:"total_usage"`
	}
	if json.Unmarshal(b2, &use) != nil || use.TotalUsage == nil {
		return model.UnavailableUsage(model.ProviderNewAPI, model.ReasonUnreadableReply)
	}

	cur := "USD"
	var sysName string
	reqSt, _ := http.NewRequestWithContext(ctx, "GET", stURL, nil)
	if st3, b3, err3 := doRequest(ctx, client, reqSt); err3 == nil && st3 == 200 {
		var statusDoc struct {
			Data *struct {
				QuotaDisplayType string `json:"quota_display_type"`
				SystemName       string `json:"system_name"`
			} `json:"data"`
		}
		if json.Unmarshal(b3, &statusDoc) == nil && statusDoc.Data != nil {
			sysName = statusDoc.Data.SystemName
			switch strings.ToUpper(statusDoc.Data.QuotaDisplayType) {
			case "CNY":
				cur = "CNY"
			case "USD":
				cur = "USD"
			case "TOKENS", "CUSTOM":
				return model.UnavailableUsage(model.ProviderNewAPI, model.ReasonNoLimitsReported)
			}
		}
	}

	rem := *sub.HardLimitUSD - (*use.TotalUsage / 100.0)
	if rem < 0 {
		rem = 0
	}
	cred := &model.CreditAmount{Amount: rem, Currency: cur}
	now := time.Now()
	u := model.ProviderUsage{
		Provider: model.ProviderNewAPI, ObservedAt: &now, State: model.StateLive,
		Plan: sysName, CreditBalance: cred.FormatShort(), CreditRemaining: cred, Source: "endpoint",
	}
	return ApplyBalanceRing(u, basis, budget)
}

// --- Kiro (kiro-cli ACP fallback) ---

func fetchKiro(ctx context.Context) model.ProviderUsage {
	if _, err := exec.LookPath("kiro-cli"); err != nil {
		return model.UnavailableUsage(model.ProviderKiro, model.ReasonLocalLoginMissing)
	}
	return model.UnavailableUsage(model.ProviderKiro, model.ReasonNoLimitsReported)
}
