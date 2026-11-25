package store

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"pulse-windows/internal/model"
)

type Cache struct {
	mu      sync.Mutex
	path    string
	Entries map[string]model.ProviderUsage `json:"entries"`
}

func NewCache() *Cache {
	c := &Cache{
		path:    filepath.Join(model.AppDataDir(), "usage-cache.json"),
		Entries: map[string]model.ProviderUsage{},
	}
	c.load()
	return c
}

func (c *Cache) load() {
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	var m map[string]model.ProviderUsage
	if json.Unmarshal(raw, &m) == nil && m != nil {
		c.Entries = m
	}
}

func (c *Cache) saveLocked() {
	raw, err := json.MarshalIndent(c.Entries, "", "  ")
	if err != nil {
		return
	}
	tmp := c.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, c.path)
	}
}

func (c *Cache) Get(id model.ProviderID) (model.ProviderUsage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	u, ok := c.Entries[string(id)]
	return u, ok
}

func (c *Cache) Reconcile(id model.ProviderID, fresh model.ProviderUsage) model.ProviderUsage {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := string(id)
	if fresh.State == model.StateLive {
		c.Entries[k] = fresh
		c.saveLocked()
		return fresh
	}
	// If transient error and we have a banked live reading within 24h whose windows haven't all reset, serve stale
	if prev, ok := c.Entries[k]; ok && prev.State == model.StateLive && prev.ObservedAt != nil {
		if time.Since(*prev.ObservedAt) < 24*time.Hour && !fresh.Reason.IsNonTransientAuth() {
			prev.State = model.StateStale
			prev.Reason = fresh.Reason
			return prev
		}
	}
	return fresh
}

// JSONReport matches Docs/json-output.md for `Pulse.exe --json`.
type JSONReport struct {
	GeneratedAt string              `json:"generatedAt"`
	Accounts    []JSONReportAccount `json:"accounts"`
}

type JSONReportAccount struct {
	ID            string             `json:"id"`
	Provider      string             `json:"provider"`
	Name          string             `json:"name"`
	Label         string             `json:"label"`
	Plan          string             `json:"plan,omitempty"`
	CreditBalance string             `json:"creditBalance,omitempty"`
	ObservedAt    *string            `json:"observedAt,omitempty"`
	AgeSeconds    *float64           `json:"ageSeconds,omitempty"`
	Source        string             `json:"source,omitempty"`
	SettingsURL   string             `json:"settingsURL"`
	Headline      *JSONReportHead    `json:"headline,omitempty"`
	Windows       []JSONReportWindow `json:"windows"`
}

type JSONReportHead struct {
	WindowID    string  `json:"windowId"`
	UsedPercent int     `json:"usedPercent"`
	Exhausted   bool    `json:"exhausted"`
	ResetsAt    *string `json:"resetsAt,omitempty"`
}

type JSONReportWindow struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	Scope         string  `json:"scope,omitempty"`
	UsedPercent   int     `json:"usedPercent"`
	UsedFraction  float64 `json:"usedFraction"`
	Exhausted     bool    `json:"exhausted"`
	WindowSeconds int     `json:"windowSeconds"`
	ReportsLength bool    `json:"reportsLength"`
	Estimated     bool    `json:"estimated"`
	EstimatedFrom string  `json:"estimatedFrom,omitempty"`
	ResetsAt      *string `json:"resetsAt,omitempty"`
}

func BuildJSONReport(settings *model.AppSettings, cache *Cache) JSONReport {
	now := time.Now().UTC()
	enabled := settings.OrderedEnabled()
	snap := settings.Snapshot()
	accounts := make([]JSONReportAccount, 0, len(enabled))
	for _, id := range enabled {
		info, _ := model.FindProvider(id)
		u, ok := cache.Get(id)
		acc := JSONReportAccount{
			ID:          string(id),
			Provider:    string(id),
			Name:        info.DisplayName,
			Label:       info.DisplayName,
			SettingsURL: "pulse://account/" + url.PathEscape(string(id)),
			Windows:     []JSONReportWindow{},
		}
		if ok {
			acc.Plan = u.Plan
			acc.CreditBalance = u.CreditBalance
			acc.Source = u.Source
			if u.ObservedAt != nil {
				obs := u.ObservedAt.UTC().Format(time.RFC3339)
				age := now.Sub(*u.ObservedAt).Seconds()
				acc.ObservedAt = &obs
				acc.AgeSeconds = &age
			}
			for _, w := range u.Windows {
				kindStr := string(w.Kind)
				if w.Kind == model.KindOther {
					kindStr = fmt.Sprintf("other:%d", w.WindowSeconds)
				}
				var res *string
				if w.ResetsAt != nil {
					s := w.ResetsAt.UTC().Format(time.RFC3339)
					res = &s
				}
				acc.Windows = append(acc.Windows, JSONReportWindow{
					ID:            w.ID,
					Kind:          kindStr,
					Scope:         w.Scope,
					UsedPercent:   w.PercentValue(false),
					UsedFraction:  w.UsedFraction,
					Exhausted:     w.IsExhausted,
					WindowSeconds: w.WindowSeconds,
					ReportsLength: w.ReportsLength,
					Estimated:     w.Estimate != "",
					EstimatedFrom: string(w.Estimate),
					ResetsAt:      res,
				})
			}
			if head := u.HeadlineWindow(snap.PinnedWindows[string(id)]); head != nil {
				var res *string
				if head.ResetsAt != nil {
					s := head.ResetsAt.UTC().Format(time.RFC3339)
					res = &s
				}
				acc.Headline = &JSONReportHead{
					WindowID:    head.ID,
					UsedPercent: head.PercentValue(false),
					Exhausted:   head.IsExhausted,
					ResetsAt:    res,
				}
			}
		}
		accounts = append(accounts, acc)
	}
	return JSONReport{
		GeneratedAt: now.Format(time.RFC3339),
		Accounts:    accounts,
	}
}
