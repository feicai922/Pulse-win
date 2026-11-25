package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pulse-windows/internal/model"
)

func fetchGemini(ctx context.Context, client *http.Client) model.ProviderUsage {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".gemini")
	if rawSet, err := os.ReadFile(filepath.Join(dir, "settings.json")); err == nil {
		var s map[string]any
		if json.Unmarshal(rawSet, &s) == nil {
			sel, _ := s["selectedAuthType"].(string)
			if sec, ok := s["security"].(map[string]any); ok {
				if auth, ok := sec["auth"].(map[string]any); ok {
					if t, ok := auth["selectedType"].(string); ok && t != "" {
						sel = t
					}
				}
			}
			if sel == "gemini-api-key" || sel == "api-key" || sel == "vertex-ai" {
				return model.UnavailableUsage(model.ProviderGemini, model.ReasonLocalLoginMissing)
			}
		}
	}

	rawCreds, err := os.ReadFile(filepath.Join(dir, "oauth_creds.json"))
	if err != nil {
		return model.UnavailableUsage(model.ProviderGemini, model.ReasonLocalLoginMissing)
	}
	var creds struct {
		AccessToken string   `json:"access_token"`
		ExpiryDate  *float64 `json:"expiry_date"`
	}
	if json.Unmarshal(rawCreds, &creds) != nil || strings.TrimSpace(creds.AccessToken) == "" {
		return model.UnavailableUsage(model.ProviderGemini, model.ReasonLocalLoginExpired)
	}
	if creds.ExpiryDate != nil && *creds.ExpiryDate > 0 {
		if time.UnixMilli(int64(*creds.ExpiryDate)).Before(time.Now()) {
			return model.UnavailableUsage(model.ProviderGemini, model.ReasonLocalLoginExpired)
		}
	}
	token := strings.TrimSpace(creds.AccessToken)

	var project, plan string
	caBody, _ := json.Marshal(map[string]any{
		"metadata": map[string]string{"ideType": "GEMINI_CLI", "pluginType": "GEMINI"},
	})
	caReq, _ := http.NewRequestWithContext(ctx, "POST", "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist", bytes.NewReader(caBody))
	caReq.Header.Set("Authorization", "Bearer "+token)
	caReq.Header.Set("Content-Type", "application/json")
	if st, b, err := doRequest(ctx, client, caReq); err == nil {
		if st == 401 {
			return model.UnavailableUsage(model.ProviderGemini, model.ReasonLocalLoginExpired)
		}
		if st >= 200 && st < 300 {
			var ca struct {
				CloudAICompanionProject string `json:"cloudaicompanionProject"`
				CurrentTier             *struct {
					Name string `json:"name"`
				} `json:"currentTier"`
			}
			if json.Unmarshal(b, &ca) == nil {
				project = ca.CloudAICompanionProject
				if ca.CurrentTier != nil {
					plan = ca.CurrentTier.Name
				}
			}
		}
	}

	qPayload := map[string]any{}
	if project != "" {
		qPayload["project"] = project
	}
	qb, _ := json.Marshal(qPayload)
	qReq, _ := http.NewRequestWithContext(ctx, "POST", "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota", bytes.NewReader(qb))
	qReq.Header.Set("Authorization", "Bearer "+token)
	qReq.Header.Set("Content-Type", "application/json")

	status, body, err := doRequest(ctx, client, qReq)
	if err != nil {
		return model.UnavailableUsage(model.ProviderGemini, model.ReasonUnreachable)
	}
	if status == 401 || status == 403 {
		return model.UnavailableUsage(model.ProviderGemini, model.ReasonLocalLoginExpired)
	}
	if status == 429 {
		return model.UnavailableUsage(model.ProviderGemini, model.ReasonRateLimited)
	}
	if status < 200 || status >= 300 {
		return model.UnavailableUsage(model.ProviderGemini, model.ReasonServerError)
	}

	var qReply struct {
		Buckets []struct {
			ModelID           string   `json:"modelId"`
			RemainingFraction *float64 `json:"remainingFraction"`
			ResetTime         string   `json:"resetTime"`
		} `json:"buckets"`
	}
	if json.Unmarshal(body, &qReply) != nil {
		return model.UnavailableUsage(model.ProviderGemini, model.ReasonUnreadableReply)
	}

	// Keep the tightest bucket per modelId
	type modelBest struct {
		rem   float64
		reset string
	}
	byModel := map[string]modelBest{}
	var order []string
	for _, b := range qReply.Buckets {
		if b.ModelID == "" || b.RemainingFraction == nil {
			continue
		}
		if cur, ok := byModel[b.ModelID]; !ok {
			byModel[b.ModelID] = modelBest{rem: *b.RemainingFraction, reset: b.ResetTime}
			order = append(order, b.ModelID)
		} else if *b.RemainingFraction < cur.rem {
			byModel[b.ModelID] = modelBest{rem: *b.RemainingFraction, reset: b.ResetTime}
		}
	}
	sort.Strings(order)
	var windows []model.UsageWindow
	for _, m := range order {
		b := byModel[m]
		windows = append(windows, model.UsageWindow{
			ID:            "gemini." + m,
			Kind:          model.KindDaily,
			Scope:         m,
			UsedFraction:  clampFraction(1 - b.rem),
			WindowSeconds: 86400,
			ResetsAt:      parseISO8601(b.reset),
			ReportsLength: true,
			IsExhausted:   b.rem <= 0,
		})
	}
	if len(windows) == 0 {
		return model.UnavailableUsage(model.ProviderGemini, model.ReasonNoLimitsReported)
	}
	now := time.Now()
	return model.ProviderUsage{
		Provider:   model.ProviderGemini,
		Windows:    windows,
		ObservedAt: &now,
		State:      model.StateLive,
		Plan:       plan,
		Source:     "endpoint",
	}
}
