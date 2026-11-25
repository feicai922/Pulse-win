package model

import (
	"fmt"
	"math"
	"sort"
	"time"
)

type WindowKind string

const (
	KindFiveHour WindowKind = "fiveHour"
	KindDaily    WindowKind = "daily"
	KindWeekly   WindowKind = "weekly"
	KindMonthly  WindowKind = "monthly"
	KindSpend    WindowKind = "spend"
	KindBalance  WindowKind = "balance"
	KindMessages WindowKind = "messages"
	KindTopUp    WindowKind = "topUp"
	KindCredits  WindowKind = "credits"
	KindOther    WindowKind = "other"
)

type EstimateKind string

const (
	EstimatePlanPrice  EstimateKind = "planPrice"
	EstimateSinceTopUp EstimateKind = "sinceTopUp"
	EstimateYourBudget EstimateKind = "yourBudget"
)

type UsageAmounts struct {
	Used      float64  `json:"used"`
	Remaining *float64 `json:"remaining,omitempty"`
	Limit     float64  `json:"limit"`
	Currency  string   `json:"currency,omitempty"`
}

type UsageWindow struct {
	ID            string        `json:"id"`
	Kind          WindowKind    `json:"kind"`
	Scope         string        `json:"scope,omitempty"`
	Label         string        `json:"label,omitempty"`
	UsedFraction  float64       `json:"usedFraction"`
	WindowSeconds int           `json:"windowSeconds"`
	ResetsAt      *time.Time    `json:"resetsAt,omitempty"`
	ReportsLength bool          `json:"reportsLength"`
	Estimate      EstimateKind  `json:"estimate,omitempty"`
	IsExhausted   bool          `json:"isExhausted"`
	Amounts       *UsageAmounts `json:"amounts,omitempty"`
}

func (w UsageWindow) RemainingFraction() float64 {
	return clamp01(1 - w.UsedFraction)
}

func (w UsageWindow) PercentValue(remaining bool) int {
	f := w.UsedFraction
	if remaining {
		f = w.RemainingFraction()
	}
	return figurePercent(f)
}

func (w UsageWindow) PercentText(remaining bool) string {
	return fmt.Sprintf("%d%%", w.PercentValue(remaining))
}

func figurePercent(fraction float64) int {
	if math.IsNaN(fraction) || math.IsInf(fraction, 0) {
		return 0
	}
	pct := clamp01(fraction) * 100
	if pct <= 0 {
		return 0
	}
	if pct >= 100 {
		return 100
	}
	v := int(math.Round(pct))
	if v < 1 {
		v = 1
	}
	if v > 99 {
		v = 99
	}
	return v
}

func (w UsageWindow) ElapsedFraction(now time.Time) (float64, bool) {
	if !w.ReportsLength || w.ResetsAt == nil || w.WindowSeconds <= 0 {
		return 0, false
	}
	rem := w.ResetsAt.Sub(now).Seconds()
	return clamp01(1 - rem/float64(w.WindowSeconds)), true
}

func (w UsageWindow) ProjectedExhaustion(now time.Time) (time.Duration, bool) {
	if w.IsExhausted || w.UsedFraction >= 1 {
		return 0, false
	}
	elapsed, ok := w.ElapsedFraction(now)
	if !ok || elapsed < 0.05 || w.UsedFraction <= 0.02 || w.ResetsAt == nil {
		return 0, false
	}
	// If burn rate >= 1, current pace will exhaust before reset
	rate := w.UsedFraction / elapsed
	if rate <= 1.0 {
		return 0, false
	}
	elapsedSec := elapsed * float64(w.WindowSeconds)
	totalNeededSec := elapsedSec / w.UsedFraction
	etaSec := totalNeededSec - elapsedSec
	if etaSec <= 0 {
		return 0, false
	}
	eta := time.Duration(etaSec * float64(time.Second))
	if eta >= w.ResetsAt.Sub(now) {
		return 0, false
	}
	return eta, true
}

func (w UsageWindow) HasTurnedOver(prevFraction float64, prevReset *time.Time) bool {
	if w.Kind == KindBalance || w.Kind == KindTopUp {
		return false
	}
	movedOn := false
	if w.ResetsAt != nil && prevReset != nil {
		movedOn = w.ResetsAt.Sub(*prevReset) > 60*time.Second
	}
	if w.Kind == KindCredits {
		return movedOn
	}
	return movedOn || (prevFraction-w.UsedFraction >= 0.4)
}

func (w UsageWindow) DisplayName(isZh bool) string {
	if w.Label != "" {
		return w.Label
	}
	var base string
	switch w.Kind {
	case KindFiveHour:
		base = pick(isZh, "5-hour limit", "5 小时限额")
	case KindDaily:
		base = pick(isZh, "Daily limit", "每日限额")
	case KindWeekly:
		base = pick(isZh, "Weekly limit", "每周限额")
	case KindMonthly:
		base = pick(isZh, "Monthly limit", "每月限额")
	case KindSpend:
		base = pick(isZh, "Spend limit", "消费限额")
	case KindBalance:
		base = pick(isZh, "Balance", "余额")
	case KindMessages:
		base = pick(isZh, "Message allowance", "消息额度")
	case KindTopUp:
		base = pick(isZh, "Top-up pack", "加油包")
	case KindCredits:
		base = pick(isZh, "Credit allowance", "积分额度")
	default:
		if w.WindowSeconds >= 86400 && w.WindowSeconds%86400 == 0 {
			days := w.WindowSeconds / 86400
			base = pick(isZh, fmt.Sprintf("%d-day limit", days), fmt.Sprintf("%d 天限额", days))
		} else {
			hours := int(math.Round(float64(w.WindowSeconds) / 3600))
			if hours < 1 {
				hours = 1
			}
			base = pick(isZh, fmt.Sprintf("%d-hour limit", hours), fmt.Sprintf("%d 小时限额", hours))
		}
	}
	if w.Scope != "" {
		base += " · " + w.Scope
	}
	switch w.Estimate {
	case EstimatePlanPrice:
		base += " · " + pick(isZh, "estimated", "估算")
	case EstimateSinceTopUp:
		base += " · " + pick(isZh, "since top-up", "自充值以来")
	case EstimateYourBudget:
		base += " · " + pick(isZh, "of your budget", "按设定预算")
	}
	return base
}

type UsageState string

const (
	StateLive        UsageState = "live"
	StateStale       UsageState = "stale"
	StateUnavailable UsageState = "unavailable"
)

type Unavailability string

const (
	ReasonLoading                 Unavailability = "loading"
	ReasonNoLimitsReported        Unavailability = "noLimitsReported"
	ReasonSignInRequired          Unavailability = "signInRequired"
	ReasonClaudeSignInRequired    Unavailability = "claudeSignInRequired"
	ReasonClaudeLoginExpired      Unavailability = "claudeLoginExpired"
	ReasonCursorSignInRequired    Unavailability = "cursorSignInRequired"
	ReasonCursorLoginExpired      Unavailability = "cursorLoginExpired"
	ReasonAntigravityNotRunning   Unavailability = "antigravityNotRunning"
	ReasonAntigravityNotAnswering Unavailability = "antigravityNotAnswering"
	ReasonAntigravityCLIMissing   Unavailability = "antigravityCLIMissing"
	ReasonGrokSignInRequired      Unavailability = "grokSignInRequired"
	ReasonGrokLoginExpired        Unavailability = "grokLoginExpired"
	ReasonLocalLoginMissing       Unavailability = "localLoginMissing"
	ReasonLocalLoginExpired       Unavailability = "localLoginExpired"
	ReasonNotSignedIn             Unavailability = "notSignedIn"
	ReasonSignedOut               Unavailability = "signedOut"
	ReasonAPIKeyMissing           Unavailability = "apiKeyMissing"
	ReasonAPIKeyRefused           Unavailability = "apiKeyRefused"
	ReasonServerAddressMissing    Unavailability = "serverAddressMissing"
	ReasonServerAddressRefused    Unavailability = "serverAddressRefused"
	ReasonNoPlan                  Unavailability = "noPlan"
	ReasonRateLimited             Unavailability = "rateLimited"
	ReasonServerError             Unavailability = "serverError"
	ReasonUnreachable             Unavailability = "unreachable"
	ReasonUnreadableReply         Unavailability = "unreadableReply"
)

func (r Unavailability) Message(isZh bool) string {
	switch r {
	case ReasonLoading:
		return pick(isZh, "Reading…", "读取中…")
	case ReasonNoLimitsReported:
		return pick(isZh, "No usage limits reported by service.", "服务未报告任何额度限额。")
	case ReasonSignInRequired:
		return pick(isZh, "Sign in with the CLI or check local credentials.", "请先在对应 CLI 登录，或检查本地凭据。")
	case ReasonClaudeSignInRequired:
		return pick(isZh, "Sign in to Claude Code first.", "请先登录 Claude Code。")
	case ReasonClaudeLoginExpired:
		return pick(isZh, "Claude Code login expired. Run `claude` to renew.", "Claude Code 登录已过期，请运行 claude 刷新。")
	case ReasonCursorSignInRequired:
		return pick(isZh, "Open Cursor and sign in to your account.", "请先打开 Cursor 并登录账号。")
	case ReasonCursorLoginExpired:
		return pick(isZh, "Cursor session expired. Open Cursor to refresh.", "Cursor 登录已过期，请打开 Cursor 刷新。")
	case ReasonAntigravityNotRunning:
		return pick(isZh, "Antigravity is not running.", "Antigravity 未运行。")
	case ReasonAntigravityNotAnswering:
		return pick(isZh, "Antigravity did not respond to quota request.", "Antigravity 未响应额度请求。")
	case ReasonAntigravityCLIMissing:
		return pick(isZh, "Antigravity CLI (`agy`) not found.", "未找到 Antigravity CLI（agy）。")
	case ReasonGrokSignInRequired:
		return pick(isZh, "Sign in to Grok CLI (~/.grok/auth.json).", "请先登录 Grok CLI（~/.grok/auth.json）。")
	case ReasonGrokLoginExpired:
		return pick(isZh, "Grok login expired. Run Grok CLI to renew.", "Grok 登录已过期，请运行 Grok CLI 刷新。")
	case ReasonLocalLoginMissing:
		return pick(isZh, "Local login file not found.", "未找到本地登录凭据。")
	case ReasonLocalLoginExpired:
		return pick(isZh, "Local login expired. Renew via its CLI/app.", "本地登录已过期，请通过对应 CLI 或应用刷新。")
	case ReasonNotSignedIn, ReasonSignedOut:
		return pick(isZh, "Not signed in. Sign in from Settings.", "未登录，请在设置中登录。")
	case ReasonAPIKeyMissing:
		return pick(isZh, "API key required. Enter it in Settings.", "需要 API Key，请在设置中填写。")
	case ReasonAPIKeyRefused:
		return pick(isZh, "API key was refused by the service.", "API Key 被服务端拒绝，请检查是否正确。")
	case ReasonServerAddressMissing:
		return pick(isZh, "Server address required in Settings.", "请在设置中填写服务器地址。")
	case ReasonServerAddressRefused:
		return pick(isZh, "Server address is invalid or refused.", "服务器地址无效或被拒绝（非内网需使用 HTTPS）。")
	case ReasonNoPlan:
		return pick(isZh, "No active coding plan on this account.", "当前账号没有生效中的订阅计划。")
	case ReasonRateLimited:
		return pick(isZh, "Rate limited by service. Will retry later.", "请求过于频繁，稍后将自动重试。")
	case ReasonServerError:
		return pick(isZh, "Service returned an error.", "服务端返回错误。")
	case ReasonUnreachable:
		return pick(isZh, "Could not reach service.", "无法连接到服务，请检查网络或代理。")
	case ReasonUnreadableReply:
		return pick(isZh, "Could not parse service response.", "无法解析服务端返回的数据。")
	default:
		return pick(isZh, "Unavailable", "不可用")
	}
}

func (r Unavailability) IsFailure() bool {
	switch r {
	case ReasonRateLimited, ReasonServerError, ReasonUnreachable, ReasonUnreadableReply:
		return true
	default:
		return false
	}
}

func (r Unavailability) IsNonTransientAuth() bool {
	switch r {
	case ReasonAPIKeyMissing, ReasonAPIKeyRefused, ReasonSignedOut, ReasonNotSignedIn,
		ReasonClaudeSignInRequired, ReasonClaudeLoginExpired,
		ReasonCursorSignInRequired, ReasonCursorLoginExpired,
		ReasonGrokSignInRequired, ReasonGrokLoginExpired,
		ReasonLocalLoginMissing, ReasonLocalLoginExpired,
		ReasonServerAddressMissing, ReasonServerAddressRefused, ReasonNoPlan:
		return true
	default:
		return false
	}
}

type CreditAmount struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

func (c *CreditAmount) FormatShort() string {
	if c == nil {
		return ""
	}
	sym := CurrencySymbol(c.Currency)
	if c.Currency == "JPY" || c.Currency == "KRW" {
		return fmt.Sprintf("%s%.0f", sym, c.Amount)
	}
	return fmt.Sprintf("%s%.2f", sym, c.Amount)
}

func CurrencySymbol(code string) string {
	switch code {
	case "CNY", "RMB":
		return "¥"
	case "JPY":
		return "¥"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	case "HKD":
		return "HK$"
	case "TWD":
		return "NT$"
	case "KRW":
		return "₩"
	case "RUB":
		return "₽"
	default:
		return "$"
	}
}

type ProviderUsage struct {
	Provider        ProviderID     `json:"provider"`
	Windows         []UsageWindow  `json:"windows"`
	ObservedAt      *time.Time     `json:"observedAt,omitempty"`
	State           UsageState     `json:"state"`
	Reason          Unavailability `json:"reason,omitempty"`
	Plan            string         `json:"plan,omitempty"`
	CreditBalance   string         `json:"creditBalance,omitempty"`
	CreditRemaining *CreditAmount  `json:"creditRemaining,omitempty"`
	Source          string         `json:"source,omitempty"`
	Diagnostic      string         `json:"diagnostic,omitempty"`
}

func UnavailableUsage(p ProviderID, reason Unavailability) ProviderUsage {
	return ProviderUsage{
		Provider: p,
		State:    StateUnavailable,
		Reason:   reason,
	}
}

func (u ProviderUsage) HeadlineWindow(pinnedID string) *UsageWindow {
	if len(u.Windows) == 0 {
		return nil
	}
	if pinnedID != "" {
		for i := range u.Windows {
			if u.Windows[i].ID == pinnedID {
				w := u.Windows[i]
				return &w
			}
		}
	}
	// Prefer exhausted or highest usedFraction
	best := u.Windows[0]
	for _, w := range u.Windows[1:] {
		if w.IsExhausted && !best.IsExhausted {
			best = w
			continue
		}
		if w.IsExhausted == best.IsExhausted && w.UsedFraction > best.UsedFraction {
			best = w
		}
	}
	return &best
}

func (u ProviderUsage) SecondWindow(pinnedID string) *UsageWindow {
	if len(u.Windows) < 2 {
		return nil
	}
	head := u.HeadlineWindow(pinnedID)
	if head == nil {
		return nil
	}
	var others []UsageWindow
	for _, w := range u.Windows {
		if w.ID != head.ID {
			others = append(others, w)
		}
	}
	if len(others) == 0 {
		return nil
	}
	sort.SliceStable(others, func(i, j int) bool {
		if others[i].IsExhausted != others[j].IsExhausted {
			return others[i].IsExhausted
		}
		return others[i].UsedFraction > others[j].UsedFraction
	})
	w := others[0]
	return &w
}

func (u ProviderUsage) RailFigure(pinnedID string, remaining bool) (fraction *float64, text string, isSpent bool, hasReading bool) {
	if u.State == StateUnavailable {
		return nil, "", false, false
	}
	if head := u.HeadlineWindow(pinnedID); head != nil {
		f := head.UsedFraction
		return &f, head.PercentText(remaining), head.IsExhausted || head.UsedFraction >= 1, true
	}
	if u.CreditRemaining != nil {
		return nil, u.CreditRemaining.FormatShort(), false, true
	}
	if u.CreditBalance != "" {
		return nil, u.CreditBalance, false, true
	}
	return nil, "", false, false
}

func FormatResetCountdown(resetsAt *time.Time, now time.Time, isZh bool) string {
	if resetsAt == nil {
		return ""
	}
	diff := resetsAt.Sub(now)
	if diff <= 0 {
		return pick(isZh, "Resets soon", "即将重置")
	}
	totalMin := int(math.Ceil(diff.Minutes()))
	days := totalMin / (24 * 60)
	hours := (totalMin % (24 * 60)) / 60
	mins := totalMin % 60
	if days > 0 {
		if hours > 0 {
			return pick(isZh, fmt.Sprintf("Resets in %dd %dh", days, hours), fmt.Sprintf("%d天%d小时后重置", days, hours))
		}
		return pick(isZh, fmt.Sprintf("Resets in %dd", days), fmt.Sprintf("%d天后重置", days))
	}
	if hours > 0 {
		if mins > 0 {
			return pick(isZh, fmt.Sprintf("Resets in %dh %dm", hours, mins), fmt.Sprintf("%d小时%d分后重置", hours, mins))
		}
		return pick(isZh, fmt.Sprintf("Resets in %dh", hours), fmt.Sprintf("%d小时后重置", hours))
	}
	return pick(isZh, fmt.Sprintf("Resets in %dm", mins), fmt.Sprintf("%d分钟后重置", mins))
}

func FormatObservedAgo(observedAt *time.Time, now time.Time, isZh bool) string {
	if observedAt == nil {
		return ""
	}
	d := now.Sub(*observedAt)
	if d < 45*time.Second {
		return pick(isZh, "Updated just now", "刚刚更新")
	}
	mins := int(d.Minutes())
	if mins < 60 {
		return pick(isZh, fmt.Sprintf("Updated %dm ago", mins), fmt.Sprintf("%d 分钟前更新", mins))
	}
	hrs := int(d.Hours())
	return pick(isZh, fmt.Sprintf("Updated %dh ago", hrs), fmt.Sprintf("%d 小时前更新", hrs))
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func pick(isZh bool, en, zh string) string {
	if isZh {
		return zh
	}
	return en
}
