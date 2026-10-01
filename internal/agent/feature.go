package agent

// Feature is one thing rush can do with a session or an agent. Every
// adapter says, feature by feature, whether it can: the core asks through
// Supports and never by the agent's name.
type Feature string

// Features, in the order they're shown.
const (
	// Running a session.
	FeatureRun          Feature = "run"       // runs headless, in rush mode: a Driver
	FeatureResume       Feature = "resume"    // picks a past session up again
	FeatureFork         Feature = "fork"      // branches a session into a new one
	FeatureRewind       Feature = "rewind"    // goes back to an earlier message, files and all
	FeatureInterrupt    Feature = "interrupt" // stops a turn and keeps the session
	FeatureGuide        Feature = "guide"     // takes a message mid-turn without stopping it
	FeatureModel        Feature = "model"     // changes model mid-session
	FeatureEffort       Feature = "effort"    // sets how hard the model thinks
	FeatureModes        Feature = "modes"     // permission modes, such as accept-edits
	FeaturePlan         Feature = "plan"      // plans before it acts
	FeatureImages       Feature = "images"
	FeatureQuestions    Feature = "questions" // asks structured questions with choices
	FeatureSubagents    Feature = "subagents"
	FeatureBackground   Feature = "background" // background tasks rush can list and stop
	FeatureContext      Feature = "context"    // a breakdown of what fills the context
	FeatureCompact      Feature = "compact"
	FeatureCommands     Feature = "commands" // slash commands and skills: a Commander
	FeatureSideQuestion Feature = "btw"      // a question aside, outside the conversation
	FeatureDirs         Feature = "dirs"     // cd and add-dir
	FeatureMCP          Feature = "mcp"
	FeatureHooks        Feature = "hooks"
	FeaturePlugins      Feature = "plugins"
	FeatureStatusLine   Feature = "statusline" // runs rush as its statusline
	FeatureScreen       Feature = "screen"     // its own TUI, shown beside rush's
	FeatureHandoffIn    Feature = "handoff"    // starts from another agent's conversation
	FeaturePort         Feature = "port"       // takes another agent's conversation whole, as its own history
	FeaturePrompt       Feature = "prompt"     // takes rush's instructions as its system prompt, not atop the first message
	FeatureSettings     Feature = "settings"   // its settings files: permission rules, env, what the Settings tab sums up
	FeatureStats        Feature = "stats"      // its own record of the account's use, by day and model

	// Finding sessions.
	FeatureLive    Feature = "live"    // sessions running outside rush: a Discoverer
	FeatureHistory Feature = "history" // past transcripts read back: a HistoryReader
	FeatureRemote  Feature = "remote"  // sessions on the agent's own servers

	// Accounts and usage.
	FeatureSwitch     Feature = "switch"  // switches between accounts: Accounts
	FeatureSignIn     Feature = "signin"  // signs in to another account: Accounts
	FeatureQuota      Feature = "quota"   // reads its limits: a QuotaSource
	FeaturePricing    Feature = "pricing" // prices its tokens: a Pricer
	FeatureEfficiency Feature = "efficiency"
	FeatureMemory     Feature = "memory" // the files it reads as memory, and the memory report
)

// FeatureInfo is a feature and what it's called.
type FeatureInfo struct {
	Feature Feature
	Label   string
}

var allFeatures = []FeatureInfo{
	{FeatureRun, "Run in rush mode"},
	{FeatureResume, "Resume"},
	{FeatureFork, "Fork"},
	{FeatureRewind, "Rewind"},
	{FeatureInterrupt, "Interrupt"},
	{FeatureGuide, "Guide mid-turn"},
	{FeatureModel, "Switch model"},
	{FeatureEffort, "Effort"},
	{FeatureModes, "Permission modes"},
	{FeaturePlan, "Plan mode"},
	{FeatureImages, "Images"},
	{FeatureQuestions, "Questions"},
	{FeatureSubagents, "Subagents"},
	{FeatureBackground, "Background tasks"},
	{FeatureContext, "Context breakdown"},
	{FeatureCompact, "Compact"},
	{FeatureCommands, "Commands and skills"},
	{FeatureSideQuestion, "Side questions (btw)"},
	{FeatureDirs, "cd and add-dir"},
	{FeatureMCP, "MCP"},
	{FeatureHooks, "Hooks"},
	{FeaturePlugins, "Plugins"},
	{FeatureStatusLine, "Statusline"},
	{FeatureScreen, "Its own screen"},
	{FeatureHandoffIn, "Hand-off in"},
	{FeaturePort, "Hand-off in whole"},
	{FeaturePrompt, "rush's instructions"},
	{FeatureSettings, "Settings files"},
	{FeatureStats, "Usage history"},
	{FeatureLive, "Running sessions"},
	{FeatureHistory, "Past sessions"},
	{FeatureRemote, "Remote sessions"},
	{FeatureSwitch, "Account switching"},
	{FeatureSignIn, "Sign-in"},
	{FeatureQuota, "Limits"},
	{FeaturePricing, "Pricing"},
	{FeatureEfficiency, "Efficiency"},
	{FeatureMemory, "Memory"},
}

// AllFeatures is every feature, in the order they're shown.
func AllFeatures() []FeatureInfo { return append([]FeatureInfo(nil), allFeatures...) }

// Label is what f is called.
func (f Feature) Label() string {
	for _, i := range allFeatures {
		if i.Feature == f {
			return i.Label
		}
	}
	return string(f)
}

// State is whether an agent has a feature.
type State int8

const (
	StateNo State = iota
	StateYes
	StatePlanned // rush means to, and hasn't yet
)

func (s State) String() string {
	switch s {
	case StateYes:
		return "yes"
	case StatePlanned:
		return "planned"
	}
	return "no"
}

// Support is how an agent has a feature, and a note to say more: "view
// only", "needs a paid plan".
type Support struct {
	Is   State
	Note string
}

// Yes, No and Planned are the supports without a note; With adds one.
var (
	Yes     = Support{Is: StateYes}
	No      = Support{Is: StateNo}
	Planned = Support{Is: StatePlanned}
)

// With is s with a note.
func (s Support) With(note string) Support { s.Note = note; return s }

// Level is how far rush's support for an agent has been tried.
type Level int8

const (
	LevelPreview Level = iota // built, not yet tried against the real program
	LevelTested               // tried against the real program
	LevelFull                 // everything rush does, and used every day
)

func (l Level) String() string {
	switch l {
	case LevelFull:
		return "full"
	case LevelTested:
		return "tested"
	}
	return "preview"
}

// Features is what agent k declares it can do; nil when it isn't
// registered.
func Features(k Kind) map[Feature]Support {
	a, ok := Get(k)
	if !ok {
		return nil
	}
	return a.Features()
}

// FeatureOf is how agent k supports f. A feature it doesn't list, or an
// agent that isn't registered, is No.
func FeatureOf(k Kind, f Feature) Support {
	return Features(k)[f]
}

// Supports is whether agent k can do f.
func Supports(k Kind, f Feature) bool { return FeatureOf(k, f).Is == StateYes }

// LevelOf is how far agent k has been tried; preview when it isn't
// registered.
func LevelOf(k Kind) Level {
	a, ok := Get(k)
	if !ok {
		return LevelPreview
	}
	return a.Level()
}
