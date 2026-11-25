package providers

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func NewHTTPClient(proxyURL string, timeout time.Duration, allowRedirects bool) *http.Client {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}
	if p := strings.TrimSpace(proxyURL); p != "" {
		if strings.EqualFold(p, "direct") || strings.EqualFold(p, "none") {
			tr.Proxy = nil
		} else if u, err := url.Parse(p); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   timeout,
	}
	if !allowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client
}

func NewLoopbackInsecureClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // loopback only
		},
	}
}

func doRequest(ctx context.Context, client *http.Client, req *http.Request) (int, []byte, error) {
	var lastErr error
	for attempt := 0; attempt <= 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return 0, nil, ctx.Err()
			case <-time.After(time.Duration(attempt*600) * time.Millisecond):
			}
		}
		r := req.Clone(ctx)
		if req.GetBody != nil {
			body, _ := req.GetBody()
			r.Body = body
		}
		resp, err := client.Do(r)
		if err != nil {
			lastErr = err
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return resp.StatusCode, data, nil
	}
	return 0, nil, lastErr
}

func parseISO8601(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return &t
		}
	}
	return nil
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	default:
		return 0, false
	}
}
