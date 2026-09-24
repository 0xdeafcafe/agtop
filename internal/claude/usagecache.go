package claude

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// UsageEvery is how old agtop's last reading of an account may get before
// it asks Anthropic again. Every agtop process shares the readings, so
// restarts, several views and --soak runs don't each ask.
const UsageEvery = 5 * time.Minute

// FetchedUsage is agtop's last good reading of an account, and until when
// Anthropic asked it not to ask again.
type FetchedUsage struct {
	Usage Usage     `json:"usage"`
	Wait  time.Time `json:"wait,omitzero"`
}

// LoadFetchedUsage reads the readings agtop keeps at path, by config folder.
func LoadFetchedUsage(path string) map[string]FetchedUsage {
	out := map[string]FetchedUsage{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

// SaveFetchedUsage records one account's entry, keeping the others.
func SaveFetchedUsage(path, key string, f FetchedUsage) error {
	all := LoadFetchedUsage(path)
	all[key] = f
	b, err := json.Marshal(all)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".usage-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	return os.Rename(tmp.Name(), path)
}

// RefreshUsage is an account's plan usage, asked of Anthropic only when the
// reading shared through path is older than UsageEvery and Anthropic hasn't
// said to wait; offline never asks. Every agtop process goes through here,
// so however many are open an account is asked once per UsageEvery.
func RefreshUsage(path string, a Account, offline bool) Usage {
	return RefreshUsageFor(path, a.ConfigDir, offline, func(ctx context.Context) (Usage, error) { return FetchUsage(ctx, a) })
}

// RefreshUsageFor is RefreshUsage for readings kept under key, fetched by
// fetch.
func RefreshUsageFor(path, key string, offline bool, fetch func(context.Context) (Usage, error)) Usage {
	f := LoadFetchedUsage(path)[key]
	u, now := f.Usage, time.Now()
	if now.Before(f.Wait) {
		u.Problem = "rate-limited to " + f.Wait.Local().Format("15:04")
	}
	if offline || now.Before(f.Wait) || now.Sub(u.FetchedAt) < UsageEvery {
		return u
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	got, err := fetch(ctx)
	var rl *ErrRateLimited
	switch {
	case err == nil:
		u = got
		_ = SaveFetchedUsage(path, key, FetchedUsage{Usage: got})
	case errors.As(err, &rl):
		_ = SaveFetchedUsage(path, key, FetchedUsage{Usage: f.Usage, Wait: rl.Until})
		u.Problem = "rate-limited to " + rl.Until.Local().Format("15:04")
	default:
		u.Problem = err.Error()
	}
	return u
}
