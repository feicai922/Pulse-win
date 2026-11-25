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

func fetchOpenCodeGo(ctx context.Context, proxyURL, enteredKey string) model.ProviderUsage {
	key := strings.TrimSpace(enteredKey)
	if key == "" {
		key = storedOpenCodeKey()
	}
	if key == "" {
		return model.UnavailableUsage(model.ProviderOpenCodeGo, model.ReasonAPIKeyMissing)
	}

	client := NewHTTPClient(proxyURL, 15*time.Second, false)
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://opencode.ai/zen/go/v1/usage", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")

	status, body, err := doRequest(ctx, client, req)
	if err != nil {
		return model.UnavailableUsage(model.ProviderOpenCodeGo, model.ReasonUnreachable)
	}
	if status == 403 {
		var fail struct {
			Error *struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &fail) == nil && fail.Error != nil && fail.Error.Type == "EntitlementError" {
			return model.UnavailableUsage(model.ProviderOpenCodeGo, model.ReasonNoPlan)
		}
		return model.UnavailableUsage(model.ProviderOpenCodeGo, model.ReasonAPIKeyRefused)
	}
	if status == 401 {
		return model.UnavailableUsage(model.ProviderOpenCodeGo, model.ReasonAPIKeyRefused)
	}
	if status == 429 {
		return model.UnavailableUsage(model.ProviderOpenCodeGo, model.ReasonRateLimited)
	}
	if status != 200 {
		return model.UnavailableUsage(model.ProviderOpenCodeGo, model.ReasonServerError)
	}

	var reply struct {
		RollingUsage *opencodeMeter `json:"rollingUsage"`
		WeeklyUsage  *opencodeMeter `json:"weeklyUsage"`
		MonthlyUsage *opencodeMeter `json:"monthlyUsage"`
	}
	if json.Unmarshal(body, &reply) != nil {
		return model.UnavailableUsage(model.ProviderOpenCodeGo, model.ReasonUnreadableReply)
	}

	var windows []model.UsageWindow
	if w := parseOpenCodeMeter(reply.RollingUsage, "rolling", model.KindFiveHour, 5*3600); w != nil {
		windows = append(windows, *w)
	}
	if w := parseOpenCodeMeter(reply.WeeklyUsage, "weekly", model.KindWeekly, 7*86400); w != nil {
		windows = append(windows, *w)
	}
	if w := parseOpenCodeMeter(reply.MonthlyUsage, "monthly", model.KindMonthly, 30*86400); w != nil {
		windows = append(windows, *w)
	}
	if len(windows) == 0 {
		return model.UnavailableUsage(model.ProviderOpenCodeGo, model.ReasonNoLimitsReported)
	}

	now := time.Now()
	return model.ProviderUsage{
		Provider:   model.ProviderOpenCodeGo,
		Windows:    windows,
		ObservedAt: &now,
		State:      model.StateLive,
		Source:     "endpoint",
	}
}

type opencodeMeter struct {
	UsagePercent *float64 `json:"usagePercent"`
	Status       string   `json:"status"`
	ResetInSec   *float64 `json:"resetInSec"`
	ResetsAt     string   `json:"resetsAt"`
}

func parseOpenCodeMeter(m *opencodeMeter, id string, kind model.WindowKind, secs int) *model.UsageWindow {
	if m == nil || m.UsagePercent == nil {
		return nil
	}
	pct := *m.UsagePercent
	resets := parseISO8601(m.ResetsAt)
	if resets == nil && m.ResetInSec != nil && *m.ResetInSec > 0 {
		t := time.Now().Add(time.Duration(*m.ResetInSec * float64(time.Second)))
		resets = &t
	}
	st := strings.ToLower(strings.TrimSpace(m.Status))
	exhausted := st != "" && st != "ok" && st != "normal"
	return &model.UsageWindow{
		ID:            id,
		Kind:          kind,
		UsedFraction:  clampFraction(pct / 100),
		WindowSeconds: secs,
		ResetsAt:      resets,
		ReportsLength: kind != model.KindMonthly,
		IsExhausted:   exhausted || pct >= 100,
	}
}

func storedOpenCodeKey() string {
	home, _ := os.UserHomeDir()
	appData := os.Getenv("APPDATA")
	localAppData := os.Getenv("LOCALAPPDATA")
	candidates := []string{
		filepath.Join(home, ".local", "share", "opencode", "auth.json"),
		filepath.Join(localAppData, "opencode", "auth.json"),
		filepath.Join(appData, "opencode", "auth.json"),
	}
	for _, p := range candidates {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		if entry, ok := doc["opencode-go"].(map[string]any); ok {
			if k, ok := entry["key"].(string); ok && strings.TrimSpace(k) != "" {
				return strings.TrimSpace(k)
			}
		}
	}
	return ""
}
