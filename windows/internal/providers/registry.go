package providers

import (
	"context"
	"time"

	"pulse-windows/internal/auth"
	"pulse-windows/internal/model"
)

func FetchProvider(ctx context.Context, id model.ProviderID, settings model.AppSettings, keys *auth.KeyStore) model.ProviderUsage {
	client := NewHTTPClient(settings.ProxyURL, 20*time.Second, true)
	keyStr := string(id)
	enteredKey := keys.Get(keyStr)
	basis := settings.BalanceBases[keyStr]
	budget := settings.BalanceBudgets[keyStr]
	addr := settings.ServerAddresses[keyStr]

	switch id {
	case model.ProviderClaudeCode:
		return fetchClaudeCode(ctx, client)
	case model.ProviderCodex:
		return fetchCodex(ctx, client)
	case model.ProviderCursor:
		return fetchCursor(ctx, settings.ProxyURL)
	case model.ProviderCopilot:
		return fetchCopilot(ctx, client, enteredKey)
	case model.ProviderAntigravity:
		return fetchAntigravity(ctx)
	case model.ProviderDeepSeek:
		return fetchDeepSeek(ctx, client, enteredKey, basis, budget)
	case model.ProviderKimiCode:
		return fetchKimiCode(ctx, client, enteredKey)
	case model.ProviderZai, model.ProviderGLMCoding:
		return fetchZai(ctx, client, id, enteredKey)
	case model.ProviderMiniMax:
		site := settings.MiniMaxSite
		if site == "china" {
			if k := keys.Get("minimax.minimaxi.com"); k != "" {
				enteredKey = k
			}
		}
		return fetchMiniMax(ctx, client, site, enteredKey)
	case model.ProviderOpenCodeGo:
		return fetchOpenCodeGo(ctx, settings.ProxyURL, enteredKey)
	case model.ProviderGemini:
		return fetchGemini(ctx, client)
	case model.ProviderCommandCode:
		return fetchCommandCode(ctx, client, enteredKey)
	case model.ProviderGrok:
		return fetchGrok(ctx, client)
	case model.ProviderVolcengine:
		return fetchVolcengine(ctx)
	case model.ProviderAlibabaCodingPlan:
		return fetchAlibabaCodingPlan(ctx, client, enteredKey)
	case model.ProviderMoonshot:
		return fetchMoonshot(ctx, client, enteredKey, basis, budget)
	case model.ProviderOpenAIPlatform:
		return fetchOpenAIPlatform(ctx, client, enteredKey, basis, budget)
	case model.ProviderKiloCode:
		return fetchKiloCode(ctx, client, enteredKey, basis, budget)
	case model.ProviderSub2API:
		return fetchSub2API(ctx, client, enteredKey, addr, basis, budget)
	case model.ProviderNewAPI:
		return fetchNewAPI(ctx, client, enteredKey, addr, basis, budget)
	case model.ProviderKiro:
		return fetchKiro(ctx)
	default:
		return model.UnavailableUsage(id, model.ReasonNoLimitsReported)
	}
}
