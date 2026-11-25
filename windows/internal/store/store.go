package store

import (
	"context"
	"sync"
	"time"

	"pulse-windows/internal/auth"
	"pulse-windows/internal/i18n"
	"pulse-windows/internal/model"
	"pulse-windows/internal/providers"
)

type NotifyFunc func(title, body string)

type UsageStore struct {
	mu           sync.RWMutex
	settings     *model.AppSettings
	keys         *auth.KeyStore
	cache        *Cache
	usages       map[model.ProviderID]model.ProviderUsage
	refreshing   map[model.ProviderID]bool
	failStreak   map[model.ProviderID]int
	warnedThresh map[string]int // windowKey -> last notified threshold pct
	warnedExh    map[string]bool
	prevWindow   map[string]struct {
		frac  float64
		reset *time.Time
	}

	onUpdate func()
	onNotify NotifyFunc
	stopCh   chan struct{}
	kickCh   chan struct{}
	interval time.Duration
}

func NewUsageStore(settings *model.AppSettings, keys *auth.KeyStore, cache *Cache) *UsageStore {
	s := &UsageStore{
		settings:     settings,
		keys:         keys,
		cache:        cache,
		usages:       map[model.ProviderID]model.ProviderUsage{},
		refreshing:   map[model.ProviderID]bool{},
		failStreak:   map[model.ProviderID]int{},
		warnedThresh: map[string]int{},
		warnedExh:    map[string]bool{},
		prevWindow: map[string]struct {
			frac  float64
			reset *time.Time
		}{},
		stopCh:   make(chan struct{}),
		kickCh:   make(chan struct{}, 1),
		interval: 2 * time.Minute,
	}
	for _, id := range settings.OrderedEnabled() {
		if cached, ok := cache.Get(id); ok && cached.State == model.StateLive {
			cached.State = model.StateStale
			s.usages[id] = cached
		} else {
			s.usages[id] = model.UnavailableUsage(id, model.ReasonLoading)
		}
	}
	return s
}

func (s *UsageStore) SetCallbacks(onUpdate func(), onNotify NotifyFunc) {
	s.mu.Lock()
	s.onUpdate = onUpdate
	s.onNotify = onNotify
	s.mu.Unlock()
}

func (s *UsageStore) Start() {
	go s.loop()
}

func (s *UsageStore) Stop() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
}

func (s *UsageStore) TriggerRefreshAll() {
	select {
	case s.kickCh <- struct{}{}:
	default:
	}
}

func (s *UsageStore) RefreshOne(id model.ProviderID) {
	go func() {
		s.fetchOne(id)
		s.emitUpdate()
	}()
}

func (s *UsageStore) Usage(id model.ProviderID) model.ProviderUsage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if u, ok := s.usages[id]; ok {
		return u
	}
	return model.UnavailableUsage(id, model.ReasonLoading)
}

func (s *UsageStore) IsRefreshing(id model.ProviderID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.refreshing[id]
}

func (s *UsageStore) CurrentInterval() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.interval
}

func (s *UsageStore) loop() {
	if len(s.settings.OrderedEnabled()) > 0 {
		s.refreshAll()
	}
	for {
		wait := s.nextInterval()
		if wait <= 0 {
			// Manual mode: sleep until kicked or stopped
			select {
			case <-s.stopCh:
				return
			case <-s.kickCh:
				s.refreshAll()
			}
			continue
		}
		timer := time.NewTimer(wait)
		select {
		case <-s.stopCh:
			timer.Stop()
			return
		case <-s.kickCh:
			timer.Stop()
			s.refreshAll()
		case <-timer.C:
			s.refreshAll()
		}
	}
}

func (s *UsageStore) nextInterval() time.Duration {
	snap := s.settings.Snapshot()
	switch snap.RefreshMode {
	case model.RefreshManual:
		return 0
	case model.Refresh5Min:
		return 5 * time.Minute
	case model.Refresh15Min:
		return 15 * time.Minute
	case model.Refresh30Min:
		return 30 * time.Minute
	default:
		// Adaptive: 2m when any active window is >=75% or close to reset, up to 30m
		d := 10 * time.Minute
		s.mu.RLock()
		for _, id := range s.settings.OrderedEnabled() {
			u := s.usages[id]
			if u.State == model.StateLive {
				for _, w := range u.Windows {
					if w.UsedFraction >= 0.75 || w.IsExhausted {
						d = 2 * time.Minute
						break
					}
					if w.ResetsAt != nil && time.Until(*w.ResetsAt) < 20*time.Minute && time.Until(*w.ResetsAt) > 0 {
						if d > 3*time.Minute {
							d = 3 * time.Minute
						}
					}
				}
			}
		}
		s.mu.RUnlock()
		s.mu.Lock()
		s.interval = d
		s.mu.Unlock()
		return d
	}
}

func (s *UsageStore) refreshAll() {
	enabled := s.settings.OrderedEnabled()
	if len(enabled) == 0 {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for _, id := range enabled {
		wg.Add(1)
		go func(pid model.ProviderID) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			s.fetchOne(pid)
		}(id)
	}
	wg.Wait()
	s.emitUpdate()
}

func (s *UsageStore) fetchOne(id model.ProviderID) {
	s.mu.Lock()
	s.refreshing[id] = true
	s.mu.Unlock()
	s.emitUpdate()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	snap := s.settings.Snapshot()
	raw := providers.FetchProvider(ctx, id, snap, s.keys)
	resolved := s.cache.Reconcile(id, raw)

	s.mu.Lock()
	s.refreshing[id] = false
	s.usages[id] = resolved
	alerts, notifyFn := s.evaluateAlertsLocked(id, raw, snap)
	s.mu.Unlock()

	if notifyFn != nil {
		for _, a := range alerts {
			notifyFn(a[0], a[1])
		}
	}
}

func (s *UsageStore) evaluateAlertsLocked(id model.ProviderID, raw model.ProviderUsage, snap model.AppSettings) ([][2]string, NotifyFunc) {
	var out [][2]string
	info, _ := model.FindProvider(id)
	name := info.DisplayName
	if name == "" {
		name = string(id)
	}
	if raw.State != model.StateLive {
		if raw.Reason.IsFailure() {
			s.failStreak[id]++
			if snap.NotifyFailure && s.failStreak[id] == 3 {
				out = append(out, [2]string{name, i18n.T("Failed to refresh quota 3 times in a row.", "连续 3 次刷新额度失败，当前显示可能是缓存数据。")})
			}
		}
		return out, s.onNotify
	}
	s.failStreak[id] = 0

	for _, w := range raw.Windows {
		wk := string(id) + ":" + w.ID
		prev, hasPrev := s.prevWindow[wk]
		if hasPrev && w.HasTurnedOver(prev.frac, prev.reset) {
			if snap.NotifyReset && (s.warnedThresh[wk] > 0 || s.warnedExh[wk]) {
				out = append(out, [2]string{name, i18n.TF("%s has reset.", "%s 已重置。", w.DisplayName(i18n.IsChinese()))})
			}
			delete(s.warnedThresh, wk)
			delete(s.warnedExh, wk)
		}
		s.prevWindow[wk] = struct {
			frac  float64
			reset *time.Time
		}{frac: w.UsedFraction, reset: w.ResetsAt}

		if (w.IsExhausted || w.UsedFraction >= 1) && snap.NotifyExhausted && !s.warnedExh[wk] {
			s.warnedExh[wk] = true
			out = append(out, [2]string{name, i18n.TF("%s is exhausted.", "%s 已用尽。", w.DisplayName(i18n.IsChinese()))})
			continue
		}

		pct := w.PercentValue(false)
		th := snap.NotifyThresholdPct
		if th <= 0 {
			th = 80
		}
		if snap.NotifyThreshold && pct >= th && s.warnedThresh[wk] < th {
			s.warnedThresh[wk] = th
			out = append(out, [2]string{name, i18n.TF("%s reached %d%% used.", "%s 已使用 %d%%。", w.DisplayName(i18n.IsChinese()), pct)})
		}
	}
	return out, s.onNotify
}

func (s *UsageStore) emitUpdate() {
	s.mu.RLock()
	fn := s.onUpdate
	s.mu.RUnlock()
	if fn != nil {
		fn()
	}
}
