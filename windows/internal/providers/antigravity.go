package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pulse-windows/internal/model"
	"pulse-windows/internal/win32"
)

func fetchAntigravity(ctx context.Context) model.ProviderUsage {
	srv := fetchAntigravityLanguageServer(ctx)
	if srv.State == model.StateLive {
		return srv
	}
	if cli := fetchAntigravityCLI(ctx); cli != nil {
		return *cli
	}
	return srv
}

type agyCandidate struct {
	pid   uint32
	ports []int
	token string
}

type agyDaemonRecord struct {
	PID       uint32 `json:"pid"`
	HTTPPort  int    `json:"httpPort"`
	HTTPSPort int    `json:"httpsPort"`
	CSRFToken string `json:"csrfToken"`
	modTime   time.Time
}

func loadAntigravityDaemonRecords() []agyDaemonRecord {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	pattern := filepath.Join(home, ".gemini", "*", "daemon", "*.json")
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) == 0 {
		return nil
	}
	var out []agyDaemonRecord
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil || info.IsDir() || info.Size() > 64*1024 {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var rec agyDaemonRecord
		if json.Unmarshal(raw, &rec) != nil || rec.PID == 0 || rec.CSRFToken == "" {
			continue
		}
		rec.modTime = info.ModTime()
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].modTime.After(out[j].modTime)
	})
	return out
}

func fetchAntigravityLanguageServer(ctx context.Context) model.ProviderUsage {
	procs := win32.FindAntigravityServers()
	daemonRecs := loadAntigravityDaemonRecords()

	daemonByPID := map[uint32][]agyDaemonRecord{}
	for _, rec := range daemonRecs {
		daemonByPID[rec.PID] = append(daemonByPID[rec.PID], rec)
	}

	var candidates []agyCandidate
	seenCandidate := map[string]bool{}
	addCandidate := func(pid uint32, ports []int, token string) {
		if token == "" || len(ports) == 0 {
			return
		}
		key := fmt.Sprintf("%d:%s", pid, token)
		if seenCandidate[key] {
			return
		}
		seenCandidate[key] = true
		candidates = append(candidates, agyCandidate{pid: pid, ports: ports, token: token})
	}

	for _, p := range procs {
		livePorts := win32.ListeningPortsForPID(p.PID)
		if len(livePorts) == 0 {
			continue
		}
		if tok := extractCSRFToken(p.CmdLine, p.Env); tok != "" {
			addCandidate(p.PID, livePorts, tok)
		}
		// Antigravity 2.34+ on Windows clears ANTIGRAVITY_SERVER_CSRF_TOKEN from its PEB
		// via SetEnvironmentVariableW after startup, but writes the live CSRF token and
		// HTTPS port to ~/.gemini/<app>/daemon/*.json keyed by PID.
		for _, rec := range daemonByPID[p.PID] {
			ports := mergePreferredPort(rec.HTTPSPort, livePorts)
			addCandidate(p.PID, ports, rec.CSRFToken)
		}
	}

	// Also consider live daemon discovery records whose PID is currently listening,
	// even if FindAntigravityServers() didn't match the executable path/cmdline.
	for _, rec := range daemonRecs {
		livePorts := win32.ListeningPortsForPID(rec.PID)
		if len(livePorts) == 0 {
			continue
		}
		ports := mergePreferredPort(rec.HTTPSPort, livePorts)
		addCandidate(rec.PID, ports, rec.CSRFToken)
	}

	if len(candidates) == 0 {
		return model.UnavailableUsage(model.ProviderAntigravity, model.ReasonAntigravityNotRunning)
	}

	client := NewLoopbackInsecureClient(4 * time.Second)
	somethingAnswered := false
	answeredEmpty := false

	for _, cand := range candidates {
		for _, port := range cand.ports {
			url := fmt.Sprintf("https://127.0.0.1:%d/exa.language_server_pb.LanguageServerService/RetrieveUserQuotaSummary", port)
			req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader([]byte("{}")))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-codeium-csrf-token", cand.token)
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			resp.Body.Close()

			if resp.StatusCode == 401 || resp.StatusCode == 403 {
				somethingAnswered = true
				continue
			}
			if resp.StatusCode != 200 {
				continue
			}
			somethingAnswered = true

			var reply antigravityQuotaReply
			if json.Unmarshal(body, &reply) != nil {
				continue
			}
			windows := parseAntigravityWindows(reply)
			if len(windows) == 0 {
				answeredEmpty = true
				continue
			}
			plan := fetchAntigravityPlan(ctx, client, port, cand.token)
			now := time.Now()
			return model.ProviderUsage{
				Provider:   model.ProviderAntigravity,
				Windows:    windows,
				ObservedAt: &now,
				State:      model.StateLive,
				Plan:       plan,
				Source:     "languageServer",
			}
		}
	}

	if answeredEmpty {
		return model.UnavailableUsage(model.ProviderAntigravity, model.ReasonNoLimitsReported)
	}
	if somethingAnswered {
		return model.UnavailableUsage(model.ProviderAntigravity, model.ReasonAntigravityNotAnswering)
	}
	return model.UnavailableUsage(model.ProviderAntigravity, model.ReasonAntigravityNotRunning)
}

func mergePreferredPort(preferred int, live []int) []int {
	if preferred <= 0 {
		return live
	}
	hasPreferred := false
	out := make([]int, 0, len(live))
	for _, p := range live {
		if p == preferred {
			hasPreferred = true
			out = append([]int{p}, out...)
		} else {
			out = append(out, p)
		}
	}
	if !hasPreferred {
		return live
	}
	return out
}

func locateAgyCLI() string {
	if p, err := exec.LookPath("agy"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".gemini", "bin", "agy.exe"),
		filepath.Join(home, ".local", "bin", "agy.exe"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func fetchAntigravityCLI(ctx context.Context) *model.ProviderUsage {
	bin := locateAgyCLI()
	if bin == "" {
		return nil
	}
	vctx, vcancel := context.WithTimeout(ctx, 8*time.Second)
	defer vcancel()
	vCmd := exec.CommandContext(vctx, bin, "--version")
	vCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	vOut, err := vCmd.Output()
	if err != nil || !agyAnswersUsageItself(string(vOut)) {
		return nil
	}

	cctx, ccancel := context.WithTimeout(ctx, 25*time.Second)
	defer ccancel()
	cmd := exec.CommandContext(cctx, bin, "--log-file", "NUL", "-p", "/usage", "--output-format", "json")
	cmd.Dir = os.TempDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var reply struct {
		Status  string `json:"status"`
		Command *struct {
			Data *struct {
				Groups []struct {
					Name    string `json:"name"`
					Buckets []struct {
						ID                string   `json:"id"`
						Window            string   `json:"window"`
						RemainingFraction *float64 `json:"remaining_fraction"`
						ResetTime         string   `json:"reset_time"`
					} `json:"buckets"`
				} `json:"groups"`
			} `json:"data"`
		} `json:"command"`
	}
	if json.Unmarshal(out, &reply) != nil || reply.Status != "SUCCESS" || reply.Command == nil || reply.Command.Data == nil {
		return nil
	}
	var q antigravityQuotaReply
	var grpList []struct {
		DisplayName string `json:"displayName"`
		Buckets     []struct {
			BucketID          string   `json:"bucketId"`
			Window            string   `json:"window"`
			RemainingFraction *float64 `json:"remainingFraction"`
			ResetTime         string   `json:"resetTime"`
		} `json:"buckets"`
	}
	for _, g := range reply.Command.Data.Groups {
		var bs []struct {
			BucketID          string   `json:"bucketId"`
			Window            string   `json:"window"`
			RemainingFraction *float64 `json:"remainingFraction"`
			ResetTime         string   `json:"resetTime"`
		}
		for _, b := range g.Buckets {
			bs = append(bs, struct {
				BucketID          string   `json:"bucketId"`
				Window            string   `json:"window"`
				RemainingFraction *float64 `json:"remainingFraction"`
				ResetTime         string   `json:"resetTime"`
			}{
				BucketID: b.ID, Window: b.Window, RemainingFraction: b.RemainingFraction, ResetTime: b.ResetTime,
			})
		}
		grpList = append(grpList, struct {
			DisplayName string `json:"displayName"`
			Buckets     []struct {
				BucketID          string   `json:"bucketId"`
				Window            string   `json:"window"`
				RemainingFraction *float64 `json:"remainingFraction"`
				ResetTime         string   `json:"resetTime"`
			} `json:"buckets"`
		}{DisplayName: g.Name, Buckets: bs})
	}
	q.Response = &struct {
		Groups []struct {
			DisplayName string `json:"displayName"`
			Buckets     []struct {
				BucketID          string   `json:"bucketId"`
				Window            string   `json:"window"`
				RemainingFraction *float64 `json:"remainingFraction"`
				ResetTime         string   `json:"resetTime"`
			} `json:"buckets"`
		} `json:"groups"`
	}{Groups: grpList}

	wins := parseAntigravityWindows(q)
	if len(wins) == 0 {
		u := model.UnavailableUsage(model.ProviderAntigravity, model.ReasonNoLimitsReported)
		u.Source = "antigravityCLI"
		return &u
	}
	now := time.Now()
	u := model.ProviderUsage{
		Provider:   model.ProviderAntigravity,
		Windows:    wins,
		ObservedAt: &now,
		State:      model.StateLive,
		Source:     "antigravityCLI",
	}
	return &u
}

func agyAnswersUsageItself(printed string) bool {
	parts := strings.Split(strings.TrimSpace(printed), ".")
	if len(parts) < 2 {
		return false
	}
	minVer := []int{1, 1, 11}
	for i := 0; i < len(minVer); i++ {
		v := 0
		if i < len(parts) {
			digits := ""
			for _, ch := range parts[i] {
				if ch >= '0' && ch <= '9' {
					digits += string(ch)
				} else {
					break
				}
			}
			if digits == "" {
				return false
			}
			v, _ = strconv.Atoi(digits)
		}
		if v != minVer[i] {
			return v > minVer[i]
		}
	}
	return true
}

func extractCSRFToken(cmdLine string, env map[string]string) string {
	fields := strings.Fields(cmdLine)
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] == "--csrf_token" {
			return strings.Trim(fields[i+1], `"`)
		}
		if strings.HasPrefix(fields[i], "--csrf_token=") {
			return strings.TrimPrefix(fields[i], "--csrf_token=")
		}
	}
	return env["ANTIGRAVITY_SERVER_CSRF_TOKEN"]
}

func fetchAntigravityPlan(ctx context.Context, client *http.Client, port int, token string) string {
	url := fmt.Sprintf("https://127.0.0.1:%d/exa.language_server_pb.LanguageServerService/GetUserStatus", port)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-codeium-csrf-token", token)
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	var st struct {
		UserStatus *struct {
			PlanStatus *struct {
				PlanInfo *struct {
					PlanName string `json:"planName"`
				} `json:"planInfo"`
			} `json:"planStatus"`
		} `json:"userStatus"`
	}
	if json.NewDecoder(resp.Body).Decode(&st) == nil &&
		st.UserStatus != nil && st.UserStatus.PlanStatus != nil && st.UserStatus.PlanStatus.PlanInfo != nil {
		return st.UserStatus.PlanStatus.PlanInfo.PlanName
	}
	return ""
}

type antigravityQuotaReply struct {
	Response *struct {
		Groups []struct {
			DisplayName string `json:"displayName"`
			Buckets     []struct {
				BucketID          string   `json:"bucketId"`
				Window            string   `json:"window"`
				RemainingFraction *float64 `json:"remainingFraction"`
				ResetTime         string   `json:"resetTime"`
			} `json:"buckets"`
		} `json:"groups"`
	} `json:"response"`
}

func parseAntigravityWindows(reply antigravityQuotaReply) []model.UsageWindow {
	if reply.Response == nil {
		return nil
	}
	var out []model.UsageWindow
	for _, grp := range reply.Response.Groups {
		scope := strings.TrimSpace(grp.DisplayName)
		words := strings.Fields(scope)
		if len(words) > 1 && strings.EqualFold(words[len(words)-1], "models") {
			scope = strings.Join(words[:len(words)-1], " ")
		}
		var grpWins []model.UsageWindow
		for _, b := range grp.Buckets {
			if b.BucketID == "" || b.RemainingFraction == nil {
				continue
			}
			kind, secs := parseAntigravityLength(b.Window)
			if secs <= 0 {
				continue
			}
			rem := *b.RemainingFraction
			grpWins = append(grpWins, model.UsageWindow{
				ID:            b.BucketID,
				Kind:          kind,
				Scope:         scope,
				UsedFraction:  clampFraction(1 - rem),
				WindowSeconds: secs,
				ResetsAt:      parseISO8601(b.ResetTime),
				ReportsLength: true,
				IsExhausted:   rem <= 0,
			})
		}
		sort.SliceStable(grpWins, func(i, j int) bool {
			return grpWins[i].WindowSeconds < grpWins[j].WindowSeconds
		})
		out = append(out, grpWins...)
	}
	return out
}

func parseAntigravityLength(w string) (model.WindowKind, int) {
	w = strings.ToLower(strings.TrimSpace(w))
	switch w {
	case "5h":
		return model.KindFiveHour, 5 * 3600
	case "weekly":
		return model.KindWeekly, 7 * 86400
	case "daily":
		return model.KindDaily, 86400
	case "monthly":
		return model.KindMonthly, 30 * 86400
	}
	if len(w) < 2 {
		return "", 0
	}
	unit := w[len(w)-1]
	n, err := strconv.Atoi(w[:len(w)-1])
	if err != nil || n <= 0 {
		return "", 0
	}
	switch unit {
	case 'h':
		if n == 5 {
			return model.KindFiveHour, 5 * 3600
		}
		return model.KindOther, n * 3600
	case 'd':
		if n == 7 {
			return model.KindWeekly, 7 * 86400
		}
		return model.KindOther, n * 86400
	default:
		return "", 0
	}
}
