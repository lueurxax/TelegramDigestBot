package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

const hoursPerDay = 24

type Config struct {
	AppEnv                        string  `env:"APP_ENV" envDefault:"local"`
	PostgresDSN                   string  `env:"POSTGRES_DSN,required"`
	BotToken                      string  `env:"BOT_TOKEN,required"`
	AdminIDs                      []int64 `env:"ADMIN_IDS" envSeparator:","`
	TargetChatID                  int64   `env:"TARGET_CHAT_ID,required"`
	TGAPIID                       int     `env:"TG_API_ID,required"`
	TGAPIHash                     string  `env:"TG_API_HASH,required"`
	TGPhone                       string  `env:"TG_PHONE"`
	TG2FAPassword                 string  `env:"TG_2FA_PASSWORD"`
	TGSessionPath                 string  `env:"TG_SESSION_PATH" envDefault:"./tg.session"`
	LLMAPIKey                     string  `env:"LLM_API_KEY,required"`
	LLMModel                      string  `env:"LLM_MODEL" envDefault:"gpt-4o-mini"`
	DigestWindow                  string  `env:"DIGEST_WINDOW" envDefault:"60m"`
	DigestTopN                    int     `env:"DIGEST_TOP_N" envDefault:"20"`
	RelevanceThreshold            float32 `env:"RELEVANCE_THRESHOLD" envDefault:"0.5"`
	ImportanceThreshold           float32 `env:"IMPORTANCE_THRESHOLD" envDefault:"0.3"`
	SimilarityThreshold           float32 `env:"SIMILARITY_THRESHOLD" envDefault:"0.65"`
	RateLimitRPS                  int     `env:"RATE_LIMIT_RPS" envDefault:"1"`
	HealthPort                    int     `env:"HEALTH_PORT" envDefault:"8080"`
	LeaderElectionEnabled         bool    `env:"LEADER_ELECTION_ENABLED" envDefault:"true"`
	LeaderElectionLeaseName       string  `env:"LEADER_ELECTION_LEASE_NAME" envDefault:"digest-scheduler-lease"`
	ReaderFetchLimit              int     `env:"READER_FETCH_LIMIT" envDefault:"20"`
	WorkerBatchSize               int     `env:"WORKER_BATCH_SIZE" envDefault:"10"`
	WorkerPollInterval            string  `env:"WORKER_POLL_INTERVAL" envDefault:"10s"`
	SchedulerTickInterval         string  `env:"SCHEDULER_TICK_INTERVAL" envDefault:"10m"`
	SchedulerCatchupWindow        string  `env:"SCHEDULER_CATCHUP_WINDOW" envDefault:"24h"`
	RelevanceGateEnabled          bool    `env:"RELEVANCE_GATE_ENABLED" envDefault:"false"`
	RelevanceGateMode             string  `env:"RELEVANCE_GATE_MODE" envDefault:"heuristic"`
	RelevanceGateModel            string  `env:"RELEVANCE_GATE_MODEL"`
	TopicDiversityCap             float32 `env:"TOPIC_DIVERSITY_CAP" envDefault:"0.30"`
	FreshnessDecayHours           int     `env:"FRESHNESS_DECAY_HOURS" envDefault:"36"`
	FreshnessFloor                float32 `env:"FRESHNESS_FLOOR" envDefault:"0.4"`
	MinTopicCount                 int     `env:"MIN_TOPIC_COUNT" envDefault:"3"`
	AutoThresholdTuningEnabled    bool    `env:"AUTO_THRESHOLD_TUNING_ENABLED" envDefault:"false"`
	ThresholdTuningStep           float32 `env:"THRESHOLD_TUNING_STEP" envDefault:"0.05"`
	ThresholdTuningMin            float32 `env:"THRESHOLD_TUNING_MIN" envDefault:"0.10"`
	ThresholdTuningMax            float32 `env:"THRESHOLD_TUNING_MAX" envDefault:"0.90"`
	ThresholdTuningNetPositive    float32 `env:"THRESHOLD_TUNING_NET_POSITIVE" envDefault:"0.20"`
	ThresholdTuningNetNegative    float32 `env:"THRESHOLD_TUNING_NET_NEGATIVE" envDefault:"-0.20"`
	ClusterSimilarityThreshold    float32 `env:"CLUSTER_SIMILARITY_THRESHOLD" envDefault:"0.75"`
	ClusterCoherenceThreshold     float32 `env:"CLUSTER_COHERENCE_THRESHOLD" envDefault:"0.70"`
	ClusterTimeWindowHours        int     `env:"CLUSTER_TIME_WINDOW_HOURS" envDefault:"36"`
	CrossTopicClusteringEnabled   bool    `env:"CROSS_TOPIC_CLUSTERING_ENABLED" envDefault:"false"`
	CrossTopicSimilarityThreshold float32 `env:"CROSS_TOPIC_SIMILARITY_THRESHOLD" envDefault:"0.90"`
	EvidenceClusteringEnabled     bool    `env:"EVIDENCE_CLUSTERING_ENABLED" envDefault:"true"`
	EvidenceClusteringBoost       float32 `env:"EVIDENCE_CLUSTERING_BOOST" envDefault:"0.15"`
	EvidenceClusteringMinScore    float32 `env:"EVIDENCE_CLUSTERING_MIN_SCORE" envDefault:"0.5"`
	RatingMinSampleChannel        int     `env:"RATING_MIN_SAMPLE_CHANNEL" envDefault:"15"`
	RatingMinSampleGlobal         int     `env:"RATING_MIN_SAMPLE_GLOBAL" envDefault:"100"`
	FactCheckGoogleEnabled        bool    `env:"FACTCHECK_GOOGLE_ENABLED" envDefault:"false"`
	FactCheckGoogleAPIKey         string  `env:"FACTCHECK_GOOGLE_API_KEY"`
	FactCheckGoogleMaxResults     int     `env:"FACTCHECK_GOOGLE_MAX_RESULTS" envDefault:"3"`
	FactCheckCacheTTLHours        int     `env:"FACTCHECK_CACHE_TTL_HOURS" envDefault:"48"`
	FactCheckGoogleRPM            int     `env:"FACTCHECK_GOOGLE_RPM" envDefault:"60"`
	FactCheckMinClaimLength       int     `env:"FACTCHECK_MIN_CLAIM_LENGTH" envDefault:"40"`
	FactCheckQueueMax             int     `env:"FACTCHECK_QUEUE_MAX" envDefault:"5000"`

	// Link enrichment
	LinkEnrichmentEnabled  bool          `env:"LINK_ENRICHMENT_ENABLED" envDefault:"false"`
	WebFetchRPS            float64       `env:"WEB_FETCH_RPS" envDefault:"2"`
	WebFetchTimeout        time.Duration `env:"WEB_FETCH_TIMEOUT" envDefault:"30s"`
	LinkCacheTTL           time.Duration `env:"LINK_CACHE_TTL" envDefault:"24h"`
	TelegramLinkCacheTTL   time.Duration `env:"TG_LINK_CACHE_TTL" envDefault:"1h"`
	MaxLinksPerMessage     int           `env:"MAX_LINKS_PER_MESSAGE" envDefault:"3"`
	MaxContentLength       int           `env:"MAX_CONTENT_LENGTH" envDefault:"5000"`
	LinkEnrichmentScope    string        `env:"LINK_ENRICHMENT_SCOPE" envDefault:"summary"`
	LinkMinWords           int           `env:"LINK_MIN_WORDS" envDefault:"80"`
	LinkSnippetMaxChars    int           `env:"LINK_SNIPPET_MAX_CHARS" envDefault:"1200"`
	LinkEmbeddingMaxMsgLen int           `env:"LINK_EMBEDDING_MAX_MSG_LEN" envDefault:"200"`
	LinkDenylistDomains    string        `env:"LINK_DENYLIST_DOMAINS" envDefault:""`

	// Source enrichment (Phase 2)
	EnrichmentEnabled           bool          `env:"ENRICHMENT_ENABLED" envDefault:"false"`
	EnrichmentMaxResults        int           `env:"ENRICHMENT_MAX_RESULTS" envDefault:"5"`
	EnrichmentCacheTTLHours     int           `env:"ENRICHMENT_CACHE_TTL_HOURS" envDefault:"168"`
	EnrichmentQueueMax          int           `env:"ENRICHMENT_QUEUE_MAX" envDefault:"5000"`
	EnrichmentMinAgreement      float32       `env:"ENRICHMENT_MIN_AGREEMENT" envDefault:"0.15"`
	EnrichmentMaxEvidenceItem   int           `env:"ENRICHMENT_MAX_EVIDENCE_PER_ITEM" envDefault:"5"`
	EnrichmentAllowlistDomains  string        `env:"ENRICHMENT_ALLOWLIST_DOMAINS" envDefault:""`
	EnrichmentDenylistDomains   string        `env:"ENRICHMENT_DENYLIST_DOMAINS" envDefault:""`
	EnrichmentDedupSimilarity   float32       `env:"ENRICHMENT_DEDUP_SIMILARITY" envDefault:"0.98"`
	EnrichmentMaxSeconds        int           `env:"ENRICHMENT_MAX_SECONDS" envDefault:"60"`
	EnrichmentProviders         string        `env:"ENRICHMENT_PROVIDERS" envDefault:""`
	EnrichmentProviderCooldown  time.Duration `env:"ENRICHMENT_PROVIDER_COOLDOWN" envDefault:"10m"`
	EnrichmentProviderGrace     time.Duration `env:"ENRICHMENT_PROVIDER_GRACE" envDefault:"0s"`
	EnrichmentQueryTranslate    bool          `env:"ENRICHMENT_QUERY_TRANSLATE" envDefault:"true"`
	EnrichmentMaxQueriesPerItem int           `env:"ENRICHMENT_MAX_QUERIES_PER_ITEM" envDefault:"5"`
	EnrichmentLanguagePolicy    string        `env:"ENRICHMENT_LANGUAGE_POLICY" envDefault:""`
	TranslationModel            string        `env:"TRANSLATION_MODEL"`
	EnrichmentDailyBudgetUSD    float64       `env:"ENRICHMENT_DAILY_BUDGET_USD" envDefault:"0"`
	EnrichmentMonthlyCapUSD     float64       `env:"ENRICHMENT_MONTHLY_CAP_USD" envDefault:"0"`
	EnrichmentEventRegistryRPM  int           `env:"ENRICHMENT_EVENTREGISTRY_RPM" envDefault:"0"`
	EnrichmentNewsAPIRPM        int           `env:"ENRICHMENT_NEWSAPI_RPM" envDefault:"0"`
	EnrichmentDailyLimit        int           `env:"ENRICHMENT_DAILY_LIMIT" envDefault:"0"`
	EnrichmentMonthlyLimit      int           `env:"ENRICHMENT_MONTHLY_LIMIT" envDefault:"0"`

	// YaCy provider
	YaCyEnabled    bool          `env:"YACY_ENABLED" envDefault:"false"`
	YaCyBaseURL    string        `env:"YACY_BASE_URL" envDefault:"http://localhost:8090"`
	YaCyTimeout    time.Duration `env:"YACY_TIMEOUT" envDefault:"30s"`
	YaCyUser       string        `env:"YACY_USER"`
	YaCyPassword   string        `env:"YACY_PASSWORD"`
	YaCyResource   string        `env:"YACY_RESOURCE" envDefault:"local"`
	YaCyMaxResults int           `env:"ENRICHMENT_YACY_MAX_RESULTS" envDefault:"10"`

	// GDELT provider
	GDELTEnabled        bool          `env:"GDELT_ENABLED" envDefault:"false"`
	GDELTRequestsPerMin int           `env:"GDELT_RPM" envDefault:"60"`
	GDELTTimeout        time.Duration `env:"GDELT_TIMEOUT" envDefault:"30s"`

	// SearxNG provider
	SearxNGEnabled bool          `env:"SEARXNG_ENABLED" envDefault:"false"`
	SearxNGBaseURL string        `env:"SEARXNG_BASE_URL" envDefault:"http://localhost:8888"`
	SearxNGTimeout time.Duration `env:"SEARXNG_TIMEOUT" envDefault:"30s"`
	SearxNGEngines string        `env:"SEARXNG_ENGINES" envDefault:""` // comma-separated, e.g. "google,duckduckgo"

	// Event Registry provider
	EventRegistryEnabled        bool          `env:"ENRICHMENT_EVENTREGISTRY_ENABLED" envDefault:"false"`
	EventRegistryAPIKey         string        `env:"ENRICHMENT_EVENTREGISTRY_API_KEY" envDefault:""`
	EventRegistryRequestsPerMin int           `env:"ENRICHMENT_EVENTREGISTRY_RPM" envDefault:"30"`
	EventRegistryTimeout        time.Duration `env:"ENRICHMENT_EVENTREGISTRY_TIMEOUT" envDefault:"30s"`

	// NewsAPI provider
	NewsAPIEnabled        bool          `env:"ENRICHMENT_NEWSAPI_ENABLED" envDefault:"false"`
	NewsAPIKey            string        `env:"ENRICHMENT_NEWSAPI_KEY" envDefault:""`
	NewsAPIRequestsPerMin int           `env:"ENRICHMENT_NEWSAPI_RPM" envDefault:"1"`
	NewsAPITimeout        time.Duration `env:"ENRICHMENT_NEWSAPI_TIMEOUT" envDefault:"30s"`

	// OpenSearch provider
	OpenSearchEnabled        bool          `env:"ENRICHMENT_OPENSEARCH_ENABLED" envDefault:"false"`
	OpenSearchBaseURL        string        `env:"ENRICHMENT_OPENSEARCH_URL" envDefault:""`
	OpenSearchIndex          string        `env:"ENRICHMENT_OPENSEARCH_INDEX" envDefault:"news"`
	OpenSearchRequestsPerMin int           `env:"ENRICHMENT_OPENSEARCH_RPM" envDefault:"60"`
	OpenSearchTimeout        time.Duration `env:"ENRICHMENT_OPENSEARCH_TIMEOUT" envDefault:"30s"`
}

func Load() (*Config, error) {
	_ = godotenv.Load() //nolint:errcheck // .env file is optional, error is expected when not present

	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parsing environment config: %w", err)
	}

	applyEnrichmentAliases(cfg)

	return cfg, nil
}

func applyEnrichmentAliases(cfg *Config) {
	applyGeneralEnrichmentAliases(cfg)
	applyYaCyAliases(cfg)
	applyGDELTAliases(cfg)
	applySearxNGAliases(cfg)
}

func applyGeneralEnrichmentAliases(cfg *Config) {
	if !hasEnv("ENRICHMENT_MAX_RESULTS") {
		setIntFromEnv("ENRICHMENT_MAX_SOURCES", &cfg.EnrichmentMaxResults)
	}

	if !hasEnv("ENRICHMENT_CACHE_TTL_HOURS") {
		setDaysAsHours("ENRICHMENT_EVIDENCE_TTL_DAYS", &cfg.EnrichmentCacheTTLHours)
	}

	if !hasEnv("ENRICHMENT_DEDUP_SIMILARITY") {
		setFloat32FromEnv("ENRICHMENT_EVIDENCE_DEDUP_SIM", &cfg.EnrichmentDedupSimilarity)
	}

	if !hasEnv("ENRICHMENT_MAX_EVIDENCE_PER_ITEM") {
		setIntFromEnv("ENRICHMENT_EVIDENCE_MAX_PER_ITEM", &cfg.EnrichmentMaxEvidenceItem)
	}
}

func applyYaCyAliases(cfg *Config) {
	if !hasEnv("YACY_ENABLED") {
		setBoolFromEnv("ENRICHMENT_YACY_ENABLED", &cfg.YaCyEnabled)
	}

	if !hasEnv("YACY_BASE_URL") {
		setStringFromEnv("ENRICHMENT_YACY_URL", &cfg.YaCyBaseURL)
	}

	if !hasEnv("YACY_TIMEOUT") {
		setDurationFromEnv("ENRICHMENT_YACY_TIMEOUT", &cfg.YaCyTimeout)
	}

	if !hasEnv("YACY_USER") {
		setStringFromEnv("ENRICHMENT_YACY_USER", &cfg.YaCyUser)
	}

	if !hasEnv("YACY_PASSWORD") {
		setStringFromEnv("ENRICHMENT_YACY_PASSWORD", &cfg.YaCyPassword)
	}

	if !hasEnv("YACY_RESOURCE") {
		setStringFromEnv("ENRICHMENT_YACY_RESOURCE", &cfg.YaCyResource)
	}

	const yacyMaxResultsKey = "ENRICHMENT_YACY_MAX_RESULTS"
	if !hasEnv(yacyMaxResultsKey) {
		setIntFromEnv(yacyMaxResultsKey, &cfg.YaCyMaxResults)
	}
}

func applyGDELTAliases(cfg *Config) {
	if !hasEnv("GDELT_RPM") {
		setIntFromEnv("ENRICHMENT_GDELT_RPM", &cfg.GDELTRequestsPerMin)
	}

	if !hasEnv("GDELT_TIMEOUT") {
		setDurationFromEnv("ENRICHMENT_GDELT_TIMEOUT", &cfg.GDELTTimeout)
	}
}

func applySearxNGAliases(cfg *Config) {
	if !hasEnv("SEARXNG_ENABLED") {
		setBoolFromEnv("ENRICHMENT_SEARXNG_ENABLED", &cfg.SearxNGEnabled)
	}

	if !hasEnv("SEARXNG_BASE_URL") {
		setStringFromEnv("ENRICHMENT_SEARXNG_URL", &cfg.SearxNGBaseURL)
	}

	if !hasEnv("SEARXNG_TIMEOUT") {
		setDurationFromEnv("ENRICHMENT_SEARXNG_TIMEOUT", &cfg.SearxNGTimeout)
	}

	if !hasEnv("SEARXNG_ENGINES") {
		setStringFromEnv("ENRICHMENT_SEARXNG_ENGINES", &cfg.SearxNGEngines)
	}
}

func hasEnv(key string) bool {
	_, ok := os.LookupEnv(key)
	return ok
}

func setStringFromEnv(key string, target *string) {
	val, ok := os.LookupEnv(key)
	if !ok {
		return
	}

	val = strings.TrimSpace(val)
	if val == "" {
		return
	}

	*target = val
}

func setBoolFromEnv(key string, target *bool) {
	val, ok := os.LookupEnv(key)
	if !ok {
		return
	}

	parsed, err := strconv.ParseBool(strings.TrimSpace(val))
	if err != nil {
		return
	}

	*target = parsed
}

func setIntFromEnv(key string, target *int) {
	val, ok := os.LookupEnv(key)
	if !ok {
		return
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil {
		return
	}

	*target = parsed
}

func setFloat32FromEnv(key string, target *float32) {
	val, ok := os.LookupEnv(key)
	if !ok {
		return
	}

	parsed, err := strconv.ParseFloat(strings.TrimSpace(val), 32)
	if err != nil {
		return
	}

	*target = float32(parsed)
}

func setDurationFromEnv(key string, target *time.Duration) {
	val, ok := os.LookupEnv(key)
	if !ok {
		return
	}

	parsed, err := time.ParseDuration(strings.TrimSpace(val))
	if err != nil {
		return
	}

	*target = parsed
}

func setDaysAsHours(key string, target *int) {
	val, ok := os.LookupEnv(key)
	if !ok {
		return
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil || parsed <= 0 {
		return
	}

	*target = parsed * hoursPerDay
}
