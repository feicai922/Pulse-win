package providers

import (
	"os"
	"os/exec"
	"path/filepath"

	"pulse-windows/internal/auth"
	"pulse-windows/internal/model"
)

// DiscoverInstalled checks presence-only hints on Windows for local tools/configs.
func DiscoverInstalled() map[model.ProviderID]bool {
	home, _ := os.UserHomeDir()
	appData := os.Getenv("APPDATA")
	localAppData := os.Getenv("LOCALAPPDATA")
	found := map[model.ProviderID]bool{}

	exists := func(p string) bool {
		if p == "" {
			return false
		}
		_, err := os.Stat(p)
		return err == nil
	}
	hasCmd := func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	}

	if exists(filepath.Join(home, ".claude")) || hasCmd("claude") {
		found[model.ProviderClaudeCode] = true
	}
	if exists(filepath.Join(home, ".codex")) || hasCmd("codex") {
		found[model.ProviderCodex] = true
	}
	if auth.HasCursorLogin() {
		found[model.ProviderCursor] = true
	}
	if exists(filepath.Join(localAppData, "Programs", "Antigravity")) ||
		exists(filepath.Join(appData, "Antigravity")) ||
		exists(filepath.Join(home, ".gemini", "bin", "agy.exe")) ||
		hasCmd("agy") {
		found[model.ProviderAntigravity] = true
	}
	if exists(filepath.Join(home, ".gemini", "oauth_creds.json")) || hasCmd("gemini") {
		found[model.ProviderGemini] = true
	}
	if exists(filepath.Join(home, ".local", "share", "opencode", "auth.json")) ||
		exists(filepath.Join(appData, "opencode", "auth.json")) ||
		hasCmd("opencode") {
		found[model.ProviderOpenCodeGo] = true
	}
	if exists(filepath.Join(home, ".commandcode", "auth.json")) || hasCmd("cmd") {
		found[model.ProviderCommandCode] = true
	}
	if exists(filepath.Join(home, ".grok", "auth.json")) || hasCmd("grok") {
		found[model.ProviderGrok] = true
	}
	if exists(filepath.Join(home, ".coding-relay", "glm-api-key")) ||
		exists(filepath.Join(home, ".config", "bigmodel", "api_key")) ||
		exists(filepath.Join(home, ".config", "zhipu", "api_key")) {
		found[model.ProviderGLMCoding] = true
	}
	if exists(filepath.Join(home, ".local", "share", "kilo", "auth.json")) {
		found[model.ProviderKiloCode] = true
	}
	if exists(filepath.Join(home, ".kiro")) || exists(filepath.Join(localAppData, "Programs", "Kiro")) || hasCmd("kiro-cli") {
		found[model.ProviderKiro] = true
	}
	return found
}
