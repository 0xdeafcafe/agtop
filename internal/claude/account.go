package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Account is one Claude Code config home. The default account is ~/.claude
// with its state in ~/.claude.json; any other lives wherever CLAUDE_CONFIG_DIR
// points and keeps its state file inside that directory.
type Account struct {
	Name      string `json:"name"`
	ConfigDir string `json:"configDir"`
}

func DefaultAccount() Account {
	home, _ := os.UserHomeDir()
	return Account{Name: "default", ConfigDir: filepath.Join(home, ".claude")}
}

func (a Account) IsDefault() bool {
	return a.ConfigDir == DefaultAccount().ConfigDir
}

func (a Account) JobsDir() string     { return filepath.Join(a.ConfigDir, "jobs") }
func (a Account) ProjectsDir() string { return filepath.Join(a.ConfigDir, "projects") }
func (a Account) RosterPath() string  { return filepath.Join(a.ConfigDir, "daemon", "roster.json") }
func (a Account) PRCachePath() string { return filepath.Join(a.ConfigDir, "gh-pr-status-cache.json") }

func (a Account) StatePath() string {
	if a.IsDefault() {
		return a.ConfigDir + ".json"
	}
	return filepath.Join(a.ConfigDir, ".claude.json")
}

// Env is what a child claude process needs to act as this account.
func (a Account) Env() []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "CLAUDE_CONFIG_DIR=") {
			env = append(env, e) // an inherited one would point at another account
		}
	}
	if a.IsDefault() {
		return env
	}
	return append(env, "CLAUDE_CONFIG_DIR="+a.ConfigDir)
}

type Window struct {
	Present  bool
	Percent  float64
	ResetsAt time.Time
}

type Usage struct {
	Email     string
	Org       string
	Plan      string
	FiveHour  Window
	SevenDay  Window
	FetchedAt time.Time
	Problem   string // why no fresh reading: not signed in, expired, rate-limited
}

type usageFile struct {
	OAuthAccount *struct {
		EmailAddress     string `json:"emailAddress"`
		OrganizationName string `json:"organizationName"`
		BillingType      string `json:"billingType"`
		SeatTier         string `json:"seatTier"`
		UserRateLimit    string `json:"userRateLimitTier"`
	} `json:"oauthAccount"`
	Cached *struct {
		FetchedAtMs int64 `json:"fetchedAtMs"`
		Utilization struct {
			FiveHour *rawWindow `json:"five_hour"`
			SevenDay *rawWindow `json:"seven_day"`
		} `json:"utilization"`
	} `json:"cachedUsageUtilization"`
}

type rawWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

func (w *rawWindow) window() Window {
	if w == nil {
		return Window{}
	}
	t, _ := time.Parse(time.RFC3339, w.ResetsAt)
	return Window{Present: true, Percent: w.Utilization, ResetsAt: t}
}

// ReadUsage reads the plan usage Claude Code already cached for this account.
// It never touches credentials or the network.
func ReadUsage(a Account) (Usage, error) {
	b, err := os.ReadFile(a.StatePath())
	if err != nil {
		return Usage{}, err
	}
	var f usageFile
	if err := json.Unmarshal(b, &f); err != nil {
		return Usage{}, err
	}
	var u Usage
	if o := f.OAuthAccount; o != nil {
		u.Email, u.Org = o.EmailAddress, o.OrganizationName
		for _, v := range []string{o.UserRateLimit, o.SeatTier, o.OrganizationName, o.BillingType} {
			if v != "" {
				u.Plan = v
				break
			}
		}
	}
	if c := f.Cached; c != nil {
		u.FiveHour = c.Utilization.FiveHour.window()
		u.SevenDay = c.Utilization.SevenDay.window()
		u.FetchedAt = time.UnixMilli(c.FetchedAtMs)
	}
	return u, nil
}
