package claude

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"time"
)

// ErrRateLimited carries when the usage endpoint says to try again.
type ErrRateLimited struct{ Until time.Time }

func (e *ErrRateLimited) Error() string { return "usage refresh rate-limited" }

var (
	ErrNotSignedIn = errors.New("not signed in")
	ErrExpired     = errors.New("sign-in expired; run Claude Code on this account once")
)

// keychainService is where Claude Code keeps an account's sign-in: the
// default account unsuffixed, any other suffixed with its folder's hash.
func (a Account) keychainService() string {
	if a.IsDefault() {
		return "Claude Code-credentials"
	}
	sum := sha256.Sum256([]byte(a.ConfigDir))
	return "Claude Code-credentials-" + hex.EncodeToString(sum[:])[:8]
}

// FetchUsage asks Anthropic for an account's plan usage with the account's
// own sign-in. The token is read for this request only and never kept.
func FetchUsage(ctx context.Context, a Account) (Usage, error) {
	raw, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", a.keychainService(), "-w").Output()
	if err != nil {
		return Usage{}, ErrNotSignedIn
	}
	var cred struct {
		OAuth struct {
			Token     string `json:"accessToken"`
			ExpiresAt int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(raw, &cred) != nil || cred.OAuth.Token == "" {
		return Usage{}, ErrNotSignedIn
	}
	if cred.OAuth.ExpiresAt > 0 && time.UnixMilli(cred.OAuth.ExpiresAt).Before(time.Now()) {
		return Usage{}, ErrExpired
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return Usage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cred.OAuth.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return Usage{}, errors.New("usage request failed")
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		return Usage{}, &ErrRateLimited{Until: retryAfter(resp.Header.Get("Retry-After"))}
	case http.StatusUnauthorized:
		return Usage{}, ErrExpired
	default:
		return Usage{}, fmt.Errorf("usage request returned %d", resp.StatusCode)
	}
	var data struct {
		FiveHour *rawWindow `json:"five_hour"`
		SevenDay *rawWindow `json:"seven_day"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&data); err != nil {
		return Usage{}, err
	}
	return Usage{FiveHour: data.FiveHour.window(), SevenDay: data.SevenDay.window(), FetchedAt: time.Now(), Fetched: true}, nil
}

func retryAfter(v string) time.Time {
	min := time.Now().Add(15 * time.Minute)
	if s, err := strconv.Atoi(v); err == nil {
		if t := time.Now().Add(time.Duration(s) * time.Second); t.After(min) {
			return t
		}
	}
	return min
}
