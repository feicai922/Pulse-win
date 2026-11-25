package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type PanelDock string

const (
	DockLeft          PanelDock = "left"
	DockRight         PanelDock = "right"
	DockTop           PanelDock = "top"
	DockBottom        PanelDock = "bottom"
	DockFloatVertical PanelDock = "floatVertical"
	DockFloatHoriz    PanelDock = "floatHorizontal"
)

func (d PanelDock) IsHorizontal() bool {
	return d == DockTop || d == DockBottom || d == DockFloatHoriz
}

func (d PanelDock) IsDocked() bool {
	return d == DockLeft || d == DockRight || d == DockTop || d == DockBottom
}

type PanelSize string

const (
	SizeSmall  PanelSize = "small"
	SizeMedium PanelSize = "medium"
	SizeLarge  PanelSize = "large"
)

func (s PanelSize) Scale() float64 {
	switch s {
	case SizeSmall:
		return 0.82
	case SizeLarge:
		return 1.22
	default:
		return 1.0
	}
}

type SurfaceStyle string

const (
	SurfaceObsidian    SurfaceStyle = "obsidian"
	SurfaceLight       SurfaceStyle = "light"
	SurfaceTranslucent SurfaceStyle = "translucent"
)

type UsageBarStyle string

const (
	BarContinuous UsageBarStyle = "continuous"
	BarSegmented  UsageBarStyle = "segmented"
)

type BalanceBasis string

const (
	BasisSinceTopUp   BalanceBasis = "sinceTopUp"
	BasisYourBudget   BalanceBasis = "yourBudget"
	BasisBalanceOnly  BalanceBasis = "balanceOnly"
)

type RefreshMode string

const (
	RefreshAdaptive RefreshMode = "adaptive"
	Refresh5Min     RefreshMode = "5m"
	Refresh15Min    RefreshMode = "15m"
	Refresh30Min    RefreshMode = "30m"
	RefreshManual   RefreshMode = "manual"
)

type Language string

const (
	LangAuto  Language = "auto"
	LangZhCN  Language = "zh-CN"
	LangEn    Language = "en"
)

type AppSettings struct {
	mu *sync.RWMutex

	HasCompletedSetup bool     `json:"hasCompletedSetup"`
	EnabledProviders  []string `json:"enabledProviders"`
	ProviderOrder     []string `json:"providerOrder"`

	// Panel placement & behavior
	IsPanelVisible        bool         `json:"isPanelVisible"`
	Dock                  PanelDock    `json:"dock"`
	HorizontalRatio       float64      `json:"horizontalRatio"`
	VerticalRatio         float64      `json:"verticalRatio"`
	MonitorIndex          int          `json:"monitorIndex"`
	SnapsToEdges          bool         `json:"snapsToEdges"`
	FollowsActiveDisplay  bool         `json:"followsActiveDisplay"`
	AutoCollapse          bool         `json:"autoCollapse"`
	HideFromScreenshots   bool         `json:"hideFromScreenshots"`

	// Appearance
	PanelSize             PanelSize     `json:"panelSize"`
	Spacing               float64       `json:"spacing"` // 0.8 (Compact), 1.0 (Standard), 1.25 (Relaxed)
	UsesRoundEnds         bool          `json:"usesRoundEnds"`
	Surface               SurfaceStyle  `json:"surface"`
	GlassOpacity          int           `json:"glassOpacity"` // 0..100
	UsageBarStyle         UsageBarStyle `json:"usageBarStyle"`
	AnimatesActivity      bool          `json:"animatesActivity"`
	ShowsRemaining        bool          `json:"showsRemaining"`
	ShowsSecondRing       bool          `json:"showsSecondRing"`
	ShowsWindowClock      bool          `json:"showsWindowClock"`
	ShowsForecast         bool          `json:"showsForecast"`
	WarningThreshold      int           `json:"warningThreshold"` // 60,70,75,80,85,90
	RingColors            map[string]string `json:"ringColors,omitempty"`
	PinnedWindows         map[string]string `json:"pinnedWindows,omitempty"`

	// Tray & App
	ShowTrayUsage         bool         `json:"showTrayUsage"`
	TrayAccount           string       `json:"trayAccount"`
	Language              Language     `json:"language"`
	LaunchAtStartup       bool         `json:"launchAtStartup"`
	GlobalHotkey          string       `json:"globalHotkey"` // e.g. "Ctrl+Alt+P"

	// Refresh & Proxy
	RefreshMode           RefreshMode  `json:"refreshMode"`
	ProxyURL              string       `json:"proxyURL"`

	// Notifications
	NotifyThreshold       bool         `json:"notifyThreshold"`
	NotifyThresholdPct    int          `json:"notifyThresholdPct"` // 75,80,90,95
	NotifyExhausted       bool         `json:"notifyExhausted"`
	NotifyReset           bool         `json:"notifyReset"`
	NotifyFailure         bool         `json:"notifyFailure"`

	// Provider-specific options
	MiniMaxSite           string             `json:"minimaxSite"` // "international" or "china"
	ServerAddresses       map[string]string  `json:"serverAddresses,omitempty"`
	BalanceBases          map[string]BalanceBasis `json:"balanceBases,omitempty"`
	BalanceBudgets        map[string]float64 `json:"balanceBudgets,omitempty"`
	ProviderSources       map[string]string  `json:"providerSources,omitempty"` // "automatic", "endpoint", "tooling", "desktopApp"
}

func DefaultSettings() *AppSettings {
	return &AppSettings{
		mu:                  &sync.RWMutex{},
		HasCompletedSetup:   false,
		EnabledProviders:    []string{},
		ProviderOrder:       defaultOrder(),
		IsPanelVisible:      true,
		Dock:                DockRight,
		HorizontalRatio:     0.5,
		VerticalRatio:       0.5,
		MonitorIndex:        0,
		SnapsToEdges:        true,
		FollowsActiveDisplay: false,
		AutoCollapse:        false,
		HideFromScreenshots: false,
		PanelSize:           SizeMedium,
		Spacing:             1.0,
		UsesRoundEnds:       false,
		Surface:             SurfaceObsidian,
		GlassOpacity:        78,
		UsageBarStyle:       BarContinuous,
		AnimatesActivity:    true,
		ShowsRemaining:      true,
		ShowsSecondRing:     false,
		ShowsWindowClock:    false,
		ShowsForecast:       false,
		WarningThreshold:    75,
		RingColors:          map[string]string{},
		PinnedWindows:       map[string]string{},
		ShowTrayUsage:       true,
		Language:            LangAuto,
		LaunchAtStartup:     false,
		GlobalHotkey:        "Ctrl+Alt+P",
		RefreshMode:         RefreshAdaptive,
		NotifyThreshold:     false,
		NotifyThresholdPct:  80,
		NotifyExhausted:     false,
		NotifyReset:         false,
		NotifyFailure:       false,
		MiniMaxSite:         "international",
		ServerAddresses:     map[string]string{},
		BalanceBases:        map[string]BalanceBasis{},
		BalanceBudgets:      map[string]float64{},
		ProviderSources:     map[string]string{},
	}
}

func defaultOrder() []string {
	out := make([]string, len(BuiltInProviders))
	for i, p := range BuiltInProviders {
		out[i] = string(p.ID)
	}
	return out
}

func AppDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, "AppData", "Roaming")
	}
	dir := filepath.Join(base, "Pulse")
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

func SettingsPath() string {
	return filepath.Join(AppDataDir(), "settings.json")
}

func LoadSettings() *AppSettings {
	s := DefaultSettings()
	data, err := os.ReadFile(SettingsPath())
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, s)
	if s.RingColors == nil {
		s.RingColors = map[string]string{}
	}
	if s.PinnedWindows == nil {
		s.PinnedWindows = map[string]string{}
	}
	if s.ServerAddresses == nil {
		s.ServerAddresses = map[string]string{}
	}
	if s.BalanceBases == nil {
		s.BalanceBases = map[string]BalanceBasis{}
	}
	if s.BalanceBudgets == nil {
		s.BalanceBudgets = map[string]float64{}
	}
	if s.ProviderSources == nil {
		s.ProviderSources = map[string]string{}
	}
	if s.WarningThreshold < 60 || s.WarningThreshold > 90 {
		s.WarningThreshold = 75
	}
	if s.GlassOpacity < 25 || s.GlassOpacity > 100 {
		s.GlassOpacity = 78
	}
	if s.Spacing <= 0 {
		s.Spacing = 1.0
	}
	if s.MiniMaxSite == "" {
		s.MiniMaxSite = "international"
	}
	if len(s.ProviderOrder) == 0 {
		s.ProviderOrder = defaultOrder()
	}
	return s
}

func (s *AppSettings) Save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	tmp := SettingsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, SettingsPath())
}

func (s *AppSettings) Snapshot() AppSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := *s
	cp.mu = nil
	cp.EnabledProviders = append([]string(nil), s.EnabledProviders...)
	cp.ProviderOrder = append([]string(nil), s.ProviderOrder...)
	cp.RingColors = cloneMap(s.RingColors)
	cp.PinnedWindows = cloneMap(s.PinnedWindows)
	cp.ServerAddresses = cloneMap(s.ServerAddresses)
	cp.BalanceBases = cloneMap(s.BalanceBases)
	cp.BalanceBudgets = cloneMap(s.BalanceBudgets)
	cp.ProviderSources = cloneMap(s.ProviderSources)
	return cp
}

func (s *AppSettings) OrderedEnabled() []ProviderID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	enabled := make(map[string]bool, len(s.EnabledProviders))
	for _, id := range s.EnabledProviders {
		enabled[id] = true
	}
	seen := map[string]bool{}
	var out []ProviderID
	for _, id := range s.ProviderOrder {
		if enabled[id] && !seen[id] {
			if _, ok := FindProvider(ProviderID(id)); ok {
				out = append(out, ProviderID(id))
				seen[id] = true
			}
		}
	}
	for _, p := range BuiltInProviders {
		id := string(p.ID)
		if enabled[id] && !seen[id] {
			out = append(out, p.ID)
			seen[id] = true
		}
	}
	return out
}

func (s *AppSettings) IsEnabled(id ProviderID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.EnabledProviders {
		if e == string(id) {
			return true
		}
	}
	return false
}

// SetEnabled updates EnabledProviders and HasCompletedSetup. Caller must hold s.mu (e.g. inside Update).
func (s *AppSettings) SetEnabled(id ProviderID, on bool) {
	key := string(id)
	var next []string
	found := false
	for _, e := range s.EnabledProviders {
		if e == key {
			found = true
			if !on {
				continue
			}
		}
		next = append(next, e)
	}
	if on && !found {
		next = append(next, key)
	}
	s.EnabledProviders = next
	if on {
		s.HasCompletedSetup = true
	}
}

func (s *AppSettings) Update(fn func(*AppSettings)) {
	s.mu.Lock()
	fn(s)
	s.mu.Unlock()
	_ = s.Save()
}

func cloneMap[K comparable, V any](src map[K]V) map[K]V {
	if src == nil {
		return map[K]V{}
	}
	out := make(map[K]V, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
