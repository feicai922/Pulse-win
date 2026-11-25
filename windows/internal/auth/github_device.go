package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	githubClientID = "Iv1.b507a08c87ecfe98"
	githubScope    = "read:user"
)

type GitHubDevicePrompt struct {
	UserCode        string
	VerificationURL string
	DeviceCode      string
	Interval        time.Duration
}

func StartGitHubDeviceLogin(ctx context.Context, client *http.Client) (*GitHubDevicePrompt, error) {
	body, _ := json.Marshal(map[string]string{
		"client_id": githubClientID,
		"scope":     githubScope,
	})
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/device/code", bytes.NewReader(body))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var reply struct {
		UserCode                string `json:"user_code"`
		DeviceCode              string `json:"device_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		Interval                int    `json:"interval"`
		ErrorDescription        string `json:"error_description"`
		Error                   string `json:"error"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, err
	}
	if reply.UserCode == "" || reply.DeviceCode == "" {
		if reply.ErrorDescription != "" {
			return nil, errors.New(reply.ErrorDescription)
		}
		if reply.Error != "" {
			return nil, errors.New(reply.Error)
		}
		return nil, errors.New("failed to start GitHub device login")
	}
	interval := reply.Interval
	if interval < 1 {
		interval = 5
	}
	if interval > 60 {
		interval = 60
	}
	url := reply.VerificationURIComplete
	if url == "" {
		url = reply.VerificationURI
	}
	return &GitHubDevicePrompt{
		UserCode:        reply.UserCode,
		VerificationURL: url,
		DeviceCode:      reply.DeviceCode,
		Interval:        time.Duration(interval) * time.Second,
	}, nil
}

func AwaitGitHubDeviceToken(ctx context.Context, client *http.Client, prompt *GitHubDevicePrompt) (string, error) {
	wait := prompt.Interval
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wait):
		}

		body, _ := json.Marshal(map[string]string{
			"client_id":   githubClientID,
			"device_code": prompt.DeviceCode,
			"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		})
		req, _ := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/oauth/access_token", bytes.NewReader(body))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()

		var reply struct {
			AccessToken      string `json:"access_token"`
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
			Interval         int    `json:"interval"`
		}
		if json.Unmarshal(raw, &reply) != nil {
			continue
		}
		if reply.AccessToken != "" {
			return reply.AccessToken, nil
		}
		switch reply.Error {
		case "", "authorization_pending":
			continue
		case "slow_down":
			if reply.Interval >= 1 && reply.Interval <= 60 {
				wait = time.Duration(reply.Interval) * time.Second
			} else {
				wait += 5 * time.Second
			}
		case "access_denied":
			return "", errors.New("sign-in was cancelled")
		case "expired_token":
			return "", errors.New("sign-in code expired")
		default:
			if reply.ErrorDescription != "" {
				return "", errors.New(reply.ErrorDescription)
			}
			return "", errors.New(reply.Error)
		}
	}
	return "", errors.New("timed out waiting for browser authorization")
}
