package bot

import "errors"

// Query limit constants.
const (
	// DefaultRatingsDays is the default number of days to look back for ratings.
	DefaultRatingsDays = 30
	// DefaultRatingsLimit is the default limit for ratings results.
	DefaultRatingsLimit = 10
	// DefaultScoresHours is the default number of hours to look back for item scores.
	DefaultScoresHours = 24
	// DefaultScoresLimit is the default limit for item scores results.
	DefaultScoresLimit = 10
	// DefaultFactCheckLimit is the default limit for recent fact check matches.
	DefaultFactCheckLimit = 5
	// DefaultFactCheckRecentHours is the default lookback for match counts.
	DefaultFactCheckRecentHours = 24
	// FactCheckClaimLimit is the max length for displaying fact check claims.
	FactCheckClaimLimit = 160
	// RecentErrorsLimit is the limit for fetching recent errors.
	RecentErrorsLimit = 10
	// SettingHistoryLimit is the limit for fetching setting history.
	SettingHistoryLimit = 20
	// DiscoveriesLimit is the limit for fetching pending discoveries.
	DiscoveriesLimit = 15
	// DiscoveryCleanupBatchSize is the number of rows to process per cleanup batch.
	DiscoveryCleanupBatchSize = 100
	// DefaultDiscoveryMinSeen is the default minimum discovery count for pending list.
	DefaultDiscoveryMinSeen = 2
	// DefaultDiscoveryMinEngagement is the default minimum engagement score for pending list.
	DefaultDiscoveryMinEngagement float32 = 50
	// RetryErrorsLimit is the limit for fetching errors when doing bulk retry.
	RetryErrorsLimit = 1000
)

// Time conversion constants.
const (
	// HoursPerDay is the number of hours in a day.
	HoursPerDay = 24
)

// Entity types.
const (
	EntityTypeBotCommand = "bot_command"
)

// Subcommand names.
const (
	SubCmdStats    = "stats"
	SubCmdAds      = "ads"
	SubCmdReset    = "reset"
	SubCmdClear    = "clear"
	SubCmdPreview  = "preview"
	SubCmdApprove  = "approve"
	SubCmdReject   = "reject"
	SubCmdCleanup  = "cleanup"
	SubCmdRejected = "show-rejected"
	SubCmdConfirm  = "confirm"
	SubCmdAuto     = "auto"
	SubCmdMode     = "mode"
	SubCmdShow     = "show"
	SubCmdWeekdays = "weekdays"
	SubCmdWeekends = "weekends"
	SubCmdTimes    = "times"
	SubCmdHourly   = "hourly"
)

// Weight override mode and toggle values.
const (
	WeightOverrideManual = "manual"
	ToggleOff            = "off"
	ToggleDisable        = "disable"
)

// Error message templates for handlers.
const (
	errMsgFailedToSaveSetting = "Failed to save setting: %v"
)

// Setting keys.
const (
	SettingTargetChatID       = "target_chat_id"
	SettingDigestWindow       = "digest_window"
	SettingFiltersAdsKeywords = "filters_ads_keywords"
	SettingFiltersAds         = "filters_ads"
	SettingDiscoveryMinSeen   = "discovery_min_seen"
	SettingDiscoveryMinScore  = "discovery_min_engagement"
	SettingDiscoveryAllow     = "discovery_description_allow"
	SettingDiscoveryDeny      = "discovery_description_deny"
)

// LLM override setting keys.
const (
	SettingLLMOverrideSummarize = "llm_override_summarize"
	SettingLLMOverrideCluster   = "llm_override_cluster"
	SettingLLMOverrideNarrative = "llm_override_narrative"
	SettingLLMOverrideTopic     = "llm_override_topic"
	SettingLLMDailyBudget       = "llm_daily_budget"
)

// Date/time formats.
const (
	DateTimeFormat     = "2006-01-02 15:04:05"
	TimeFormat         = "15:04"
	DateFormatShort    = "Jan 02"
	discoveryUnknown   = "Unknown"
	discoveryUnknownLC = "unknown"
)

// Discovery formatting templates.
const (
	discoverPreviewUsage    = "Usage: <code>/discover preview &lt;@username&gt;</code>"
	discoveryTitleIdentFmt  = "• <b>%s</b> (%s)\n"
	discoveryLastSeenFormat = DateFormatShort
)

const (
	schedulePreviewDefault = 5
	schedulePreviewMax     = 20
)

const (
	errInvalidScheduleFmt   = "❌ Invalid schedule: %s"
	scheduleTimezoneLineFmt = "• Timezone: <code>%s</code>\n"
)

var errInvalidPreviewCount = errors.New("invalid preview count")

// Prompt template constants.
const (
	PromptActiveKeyFmt = "prompt:%s:active"
	PromptKeyFmt       = "prompt:%s:%s"
)

// promptBases is the list of valid prompt base names.
var promptBases = []string{"summarize", "narrative", "cluster_summary", "cluster_topic", "relevance_gate"}

// Error message formats and strings.
const (
	ErrSavingFmt                      = "❌ Error saving %s: %s"
	ErrFetchingChannelsFmt            = "❌ Error fetching channels: %s"
	ErrFetchingFactCheckMatchesFmt    = "❌ Error fetching recent fact check matches: %s"
	ErrFetchingAdsKeywords            = "❌ Error fetching ads keywords."
	ErrUnknownBaseFmt                 = "Unknown base. Use: <code>%s</code>"
	ErrChannelNotFoundFmt             = "Channel <code>%s</code> not found."
	ErrGenericFmt                     = "Error: %s"
	ErrNoRows                         = "no rows"
	MsgCouldNotGetImportanceThreshold = "could not get importance threshold from DB"
	MsgScoresDebugUsage               = "Usage: <code>/scores debug [hours]</code>"
	MsgScoresDebugReasonsUsage        = "Usage: <code>/scores debug reasons [hours]</code>"
)

// Status strings.
const (
	StatusEnabled  = "ENABLED"
	StatusDisabled = "DISABLED"
)

// Format and default value constants.
const (
	DateFormatYMD          = "2006-01-02"
	DefaultReliabilityZero = 0.0
)

// Help messages.
const (
	TipSettingsReset = "\n💡 <i>Use <code>/settings reset &lt;key&gt;</code> to return a setting to its default environment value.</i>"
)

// Discovery keyword filter messages.
const (
	errKeywordAlreadyExists = "❌ Keyword already exists."
	errKeywordNotFound      = "❌ Keyword not found."
)

// Discovery filter subcommand names.
const (
	filterTypeAllow = "allow"
	filterTypeDeny  = "deny"
	subCmdHelp      = "help"
	subCmdSet       = "set"
	subCmdReset     = "reset"
)

// Preview field constants.
const (
	previewYes = "yes"
	previewNo  = "no"
)

// Error message formats.
const (
	fmtErrRetryingItems = "❌ Error retrying items: %s"
)
