package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"pulse-windows/internal/model"
)

var baselineMu sync.Mutex

type baselineMark struct {
	Peak  float64   `json:"peak"`
	SetAt time.Time `json:"setAt"`
}

func loadBalanceBaselines() map[string]baselineMark {
	baselineMu.Lock()
	defer baselineMu.Unlock()
	p := filepath.Join(model.AppDataDir(), "balance-baseline.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		return map[string]baselineMark{}
	}
	var m map[string]baselineMark
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return map[string]baselineMark{}
	}
	return m
}

func advanceBalanceBaseline(key string, balance float64) baselineMark {
	baselineMu.Lock()
	defer baselineMu.Unlock()
	p := filepath.Join(model.AppDataDir(), "balance-baseline.json")
	m := map[string]baselineMark{}
	if raw, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(raw, &m)
	}
	cur, ok := m[key]
	if !ok || balance > cur.Peak {
		cur = baselineMark{Peak: balance, SetAt: time.Now()}
		m[key] = cur
		if b, err := json.MarshalIndent(m, "", "  "); err == nil {
			_ = os.WriteFile(p, b, 0o600)
		}
	}
	return cur
}

func ApplyBalanceRing(usage model.ProviderUsage, basis model.BalanceBasis, budget float64) model.ProviderUsage {
	if usage.State != model.StateLive || len(usage.Windows) > 0 || usage.CreditRemaining == nil {
		return usage
	}
	if basis == "" {
		basis = model.BasisSinceTopUp
	}
	bal := usage.CreditRemaining.Amount
	cur := usage.CreditRemaining.Currency
	if cur == "" {
		cur = "USD"
	}
	mark := advanceBalanceBaseline(string(usage.Provider)+"."+cur, bal)

	var denom float64
	var est model.EstimateKind
	switch basis {
	case model.BasisBalanceOnly:
		return usage
	case model.BasisYourBudget:
		if budget <= 0 {
			return usage
		}
		denom = budget
		est = model.EstimateYourBudget
	default:
		if mark.Peak <= 0 {
			return usage
		}
		denom = mark.Peak
		est = model.EstimateSinceTopUp
	}

	used := denom - bal
	if used < 0 {
		used = 0
	}
	frac := clampFraction(used / denom)
	// Balance rings cap at 0.99 unless provider explicitly marks unavailable/exhausted
	if frac >= 1 {
		frac = 0.99
	}
	rem := bal
	usage.Windows = []model.UsageWindow{{
		ID:            string(usage.Provider) + ".balance",
		Kind:          model.KindSpend,
		UsedFraction:  frac,
		WindowSeconds: 0,
		ReportsLength: false,
		Estimate:      est,
		Amounts: &model.UsageAmounts{
			Used:      used,
			Remaining: &rem,
			Limit:     denom,
			Currency:  cur,
		},
	}}
	return usage
}

func fetchDeepSeek(ctx context.Context, client *http.Client, apiKey string, basis model.BalanceBasis, budget float64) model.ProviderUsage {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return model.UnavailableUsage(model.ProviderDeepSeek, model.ReasonAPIKeyMissing)
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.deepseek.com/user/balance", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")

	status, body, err := doRequest(ctx, client, req)
	if err != nil {
		return model.UnavailableUsage(model.ProviderDeepSeek, model.ReasonUnreachable)
	}
	switch status {
	case 200:
	case 401, 403:
		return model.UnavailableUsage(model.ProviderDeepSeek, model.ReasonAPIKeyRefused)
	case 429:
		return model.UnavailableUsage(model.ProviderDeepSeek, model.ReasonRateLimited)
	default:
		return model.UnavailableUsage(model.ProviderDeepSeek, model.ReasonServerError)
	}

	var reply struct {
		IsAvailable  *bool `json:"is_available"`
		BalanceInfos []struct {
			Currency        string `json:"currency"`
			TotalBalance    string `json:"total_balance"`
			GrantedBalance  string `json:"granted_balance"`
			ToppedUpBalance string `json:"topped_up_balance"`
		} `json:"balance_infos"`
	}
	if json.Unmarshal(body, &reply) != nil {
		return model.UnavailableUsage(model.ProviderDeepSeek, model.ReasonUnreadableReply)
	}
	if len(reply.BalanceInfos) == 0 {
		return model.UnavailableUsage(model.ProviderDeepSeek, model.ReasonNoLimitsReported)
	}
	chosen := reply.BalanceInfos[0]
	chosenVal, err := strconv.ParseFloat(strings.TrimSpace(chosen.TotalBalance), 64)
	if err != nil {
		return model.UnavailableUsage(model.ProviderDeepSeek, model.ReasonNoLimitsReported)
	}
	for _, info := range reply.BalanceInfos {
		if v, err := strconv.ParseFloat(strings.TrimSpace(info.TotalBalance), 64); err == nil && v > 0 {
			chosen = info
			chosenVal = v
			break
		}
	}

	cred := &model.CreditAmount{Amount: chosenVal, Currency: chosen.Currency}
	now := time.Now()
	usage := model.ProviderUsage{
		Provider:        model.ProviderDeepSeek,
		ObservedAt:      &now,
		State:           model.StateLive,
		CreditBalance:   cred.FormatShort(),
		CreditRemaining: cred,
		Source:          "endpoint",
	}
	usage = ApplyBalanceRing(usage, basis, budget)
	if reply.IsAvailable != nil && !*reply.IsAvailable && len(usage.Windows) > 0 {
		usage.Windows[0].IsExhausted = true
		usage.Windows[0].UsedFraction = 1
	}
	return usage
}
