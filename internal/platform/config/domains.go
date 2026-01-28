package config

import "time"

// DatabaseConfig holds database connection settings.
type DatabaseConfig struct {
	PostgresDSN       string        `env:"POSTGRES_DSN,required"`
	MaxConnections    int32         `env:"DB_MAX_CONNECTIONS" envDefault:"25"`
	MinConnections    int32         `env:"DB_MIN_CONNECTIONS" envDefault:"5"`
	MaxConnIdleTime   time.Duration `env:"DB_MAX_CONN_IDLE_TIME" envDefault:"30m"`
	MaxConnLifetime   time.Duration `env:"DB_MAX_CONN_LIFETIME" envDefault:"1h"`
	HealthCheckPeriod time.Duration `env:"DB_HEALTH_CHECK_PERIOD" envDefault:"1m"`
}

// TelegramBotConfig holds Telegram bot settings.
type TelegramBotConfig struct {
	Token        string  `env:"BOT_TOKEN,required"`
	Username     string  `env:"TELEGRAM_BOT_USERNAME" envDefault:""`
	AdminIDs     []int64 `env:"ADMIN_IDS" envSeparator:","`
	TargetChatID int64   `env:"TARGET_CHAT_ID,required"`
}

// TelegramMTProtoConfig holds Telegram MTProto API settings.
type TelegramMTProtoConfig struct {
	APIID       int    `env:"TG_API_ID,required"`
	APIHash     string `env:"TG_API_HASH,required"`
	Phone       string `env:"TG_PHONE"`
	Password2FA string `env:"TG_2FA_PASSWORD"`
	SessionPath string `env:"TG_SESSION_PATH" envDefault:"./tg.session"`
}

// LLMConfig holds LLM provider settings.
type LLMConfig struct {
	// Primary OpenAI
	APIKey string `env:"LLM_API_KEY"`
	Model  string `env:"LLM_MODEL" envDefault:"gpt-4o-mini"`

	// Alternative providers
	AnthropicAPIKey  string `env:"ANTHROPIC_API_KEY" envDefault:""`
	GoogleAPIKey     string `env:"GOOGLE_API_KEY" envDefault:""`
	GoogleAPIKeyPaid string `env:"GOOGLE_API_KEY_PAID" envDefault:""`
	CohereAPIKey     string `env:"COHERE_API_KEY" envDefault:""`
	OpenRouterAPIKey string `env:"OPENROUTER_API_KEY" envDefault:""`

	// Circuit breaker
	CircuitThreshold int           `env:"LLM_CIRCUIT_THRESHOLD" envDefault:"5"`
	CircuitTimeout   time.Duration `env:"LLM_CIRCUIT_TIMEOUT" envDefault:"1m"`

	// Per-task model overrides
	SummarizeModel     string `env:"LLM_SUMMARIZE_MODEL" envDefault:""`
	ClusterModel       string `env:"LLM_CLUSTER_MODEL" envDefault:""`
	NarrativeModel     string `env:"LLM_NARRATIVE_MODEL" envDefault:""`
	TranslateModel     string `env:"LLM_TRANSLATE_MODEL" envDefault:""`
	CompleteModel      string `env:"LLM_COMPLETE_MODEL" envDefault:""`
	RelevanceGateModel string `env:"LLM_RELEVANCE_GATE_MODEL" envDefault:""`
	CompressModel      string `env:"LLM_COMPRESS_MODEL" envDefault:""`
	TranslationModel   string `env:"TRANSLATION_MODEL"`
}

// EmbeddingConfig holds embedding provider settings.
type EmbeddingConfig struct {
	OpenAIModel      string        `env:"OPENAI_EMBEDDING_MODEL" envDefault:"text-embedding-3-large"`
	OpenAIDimensions int           `env:"OPENAI_EMBEDDING_DIMENSIONS" envDefault:"1536"`
	CohereModel      string        `env:"COHERE_EMBEDDING_MODEL" envDefault:"embed-multilingual-v3.0"`
	ProviderOrder    string        `env:"EMBEDDING_PROVIDER_ORDER" envDefault:"openai,cohere,google"`
	CircuitThreshold int           `env:"EMBEDDING_CIRCUIT_THRESHOLD" envDefault:"5"`
	CircuitTimeout   time.Duration `env:"EMBEDDING_CIRCUIT_TIMEOUT" envDefault:"1m"`
}

// DigestConfig holds digest generation settings.
type DigestConfig struct {
	Window               string        `env:"DIGEST_WINDOW" envDefault:"60m"`
	TopN                 int           `env:"DIGEST_TOP_N" envDefault:"20"`
	TickInterval         string        `env:"SCHEDULER_TICK_INTERVAL" envDefault:"10m"`
	CatchupWindow        string        `env:"SCHEDULER_CATCHUP_WINDOW" envDefault:"24h"`
	TimeToAlertThreshold time.Duration `env:"TIME_TO_DIGEST_ALERT_THRESHOLD" envDefault:"0"`
}

// ClusterConfig holds clustering settings.
type ClusterConfig struct {
	SimilarityThreshold  float32 `env:"CLUSTER_SIMILARITY_THRESHOLD" envDefault:"0.75"`
	CoherenceThreshold   float32 `env:"CLUSTER_COHERENCE_THRESHOLD" envDefault:"0.70"`
	TimeWindowHours      int     `env:"CLUSTER_TIME_WINDOW_HOURS" envDefault:"36"`
	CrossTopicEnabled    bool    `env:"CROSS_TOPIC_CLUSTERING_ENABLED" envDefault:"false"`
	CrossTopicSimilarity float32 `env:"CROSS_TOPIC_SIMILARITY_THRESHOLD" envDefault:"0.90"`
	EvidenceEnabled      bool    `env:"EVIDENCE_CLUSTERING_ENABLED" envDefault:"true"`
	EvidenceBoost        float32 `env:"EVIDENCE_CLUSTERING_BOOST" envDefault:"0.15"`
	EvidenceMinScore     float32 `env:"EVIDENCE_CLUSTERING_MIN_SCORE" envDefault:"0.5"`
}

// EnrichmentConfig holds source enrichment settings.
type EnrichmentConfig struct {
	Enabled            bool          `env:"ENRICHMENT_ENABLED" envDefault:"false"`
	MaxResults         int           `env:"ENRICHMENT_MAX_RESULTS" envDefault:"5"`
	CacheTTLHours      int           `env:"ENRICHMENT_CACHE_TTL_HOURS" envDefault:"168"`
	QueueMax           int           `env:"ENRICHMENT_QUEUE_MAX" envDefault:"5000"`
	MinAgreement       float32       `env:"ENRICHMENT_MIN_AGREEMENT" envDefault:"0.15"`
	MaxEvidencePerItem int           `env:"ENRICHMENT_MAX_EVIDENCE_PER_ITEM" envDefault:"5"`
	AllowlistDomains   string        `env:"ENRICHMENT_ALLOWLIST_DOMAINS" envDefault:""`
	DenylistDomains    string        `env:"ENRICHMENT_DENYLIST_DOMAINS" envDefault:""`
	DedupSimilarity    float32       `env:"ENRICHMENT_DEDUP_SIMILARITY" envDefault:"0.98"`
	MaxSeconds         int           `env:"ENRICHMENT_MAX_SECONDS" envDefault:"180"`
	Providers          string        `env:"ENRICHMENT_PROVIDERS" envDefault:""`
	ProviderCooldown   time.Duration `env:"ENRICHMENT_PROVIDER_COOLDOWN" envDefault:"10m"`
	ProviderGrace      time.Duration `env:"ENRICHMENT_PROVIDER_GRACE" envDefault:"0s"`
	QueryTranslate     bool          `env:"ENRICHMENT_QUERY_TRANSLATE" envDefault:"true"`
	QueryLLM           bool          `env:"ENRICHMENT_QUERY_LLM" envDefault:"false"`
	QueryLLMModel      string        `env:"ENRICHMENT_QUERY_LLM_MODEL" envDefault:""`
	MaxQueriesPerItem  int           `env:"ENRICHMENT_MAX_QUERIES_PER_ITEM" envDefault:"5"`
	LanguagePolicy     string        `env:"ENRICHMENT_LANGUAGE_POLICY" envDefault:""`
	LLMTimeout         time.Duration `env:"ENRICHMENT_LLM_TIMEOUT" envDefault:"45s"`
	DailyBudgetUSD     float64       `env:"ENRICHMENT_DAILY_BUDGET_USD" envDefault:"0"`
	MonthlyCapUSD      float64       `env:"ENRICHMENT_MONTHLY_CAP_USD" envDefault:"0"`
	EventRegistryRPM   int           `env:"ENRICHMENT_EVENTREGISTRY_RPM" envDefault:"0"`
	NewsAPIRPM         int           `env:"ENRICHMENT_NEWSAPI_RPM" envDefault:"0"`
	DailyLimit         int           `env:"ENRICHMENT_DAILY_LIMIT" envDefault:"0"`
	MonthlyLimit       int           `env:"ENRICHMENT_MONTHLY_LIMIT" envDefault:"0"`
}

// ExpandedViewConfig holds expanded view (detail page) settings.
type ExpandedViewConfig struct {
	Enabled           bool   `env:"EXPANDED_VIEW_ENABLED" envDefault:"false"`
	BaseURL           string `env:"EXPANDED_VIEW_BASE_URL" envDefault:""`
	SigningSecret     string `env:"EXPANDED_VIEW_SIGNING_SECRET" envDefault:""`
	TTLHours          int    `env:"EXPANDED_VIEW_TTL_HOURS" envDefault:"72"`
	RequireAdmin      bool   `env:"EXPANDED_VIEW_REQUIRE_ADMIN" envDefault:"true"`
	AllowSystemTokens bool   `env:"EXPANDED_VIEW_ALLOW_SYSTEM_TOKENS" envDefault:"false"`

	// Apple Shortcuts integration
	ShortcutEnabled   bool   `env:"EXPANDED_CHATGPT_SHORTCUT_ENABLED" envDefault:"false"`
	ShortcutName      string `env:"EXPANDED_CHATGPT_SHORTCUT_NAME" envDefault:"Ask ChatGPT"`
	ShortcutICloudURL string `env:"EXPANDED_CHATGPT_SHORTCUT_ICLOUD_URL" envDefault:""`
	ShortcutMaxChars  int    `env:"EXPANDED_SHORTCUT_URL_MAX_CHARS" envDefault:"2000"`
}

// CrawlerConfig holds web crawler settings.
type CrawlerConfig struct {
	Depth           int           `env:"CRAWL_DEPTH" envDefault:"2"`
	RateLimitRPS    float64       `env:"CRAWL_RATE_LIMIT_RPS" envDefault:"2"`
	BatchSize       int           `env:"CRAWL_BATCH_SIZE" envDefault:"10"`
	ClaimTTL        time.Duration `env:"CRAWL_CLAIM_TTL" envDefault:"5m"`
	UserAgent       string        `env:"CRAWL_USER_AGENT" envDefault:"TelegramDigestBot/1.0"`
	SeedsFile       string        `env:"CRAWL_SEEDS_FILE" envDefault:"/config/seeds.txt"`
	QueueMaxPending int           `env:"CRAWLER_QUEUE_MAX_PENDING" envDefault:"10000"`
}

// ProviderYaCyConfig holds YaCy search provider settings.
type ProviderYaCyConfig struct {
	Enabled    bool          `env:"YACY_ENABLED" envDefault:"false"`
	BaseURL    string        `env:"YACY_BASE_URL" envDefault:"http://localhost:8090"`
	Timeout    time.Duration `env:"YACY_TIMEOUT" envDefault:"30s"`
	User       string        `env:"YACY_USER"`
	Password   string        `env:"YACY_PASSWORD"`
	Resource   string        `env:"YACY_RESOURCE" envDefault:"local"`
	MaxResults int           `env:"ENRICHMENT_YACY_MAX_RESULTS" envDefault:"10"`
}

// ProviderSolrConfig holds Solr search provider settings.
type ProviderSolrConfig struct {
	Enabled    bool          `env:"SOLR_ENABLED" envDefault:"false"`
	BaseURL    string        `env:"SOLR_URL" envDefault:"http://solr:8983/solr/news"`
	Timeout    time.Duration `env:"SOLR_TIMEOUT" envDefault:"10s"`
	MaxResults int           `env:"SOLR_MAX_RESULTS" envDefault:"10"`
}

// DatabaseCfg returns the database configuration extracted from Config.
func (c *Config) DatabaseCfg() DatabaseConfig {
	return DatabaseConfig{
		PostgresDSN:       c.PostgresDSN,
		MaxConnections:    c.DBMaxConnections,
		MinConnections:    c.DBMinConnections,
		MaxConnIdleTime:   c.DBMaxConnIdleTime,
		MaxConnLifetime:   c.DBMaxConnLifetime,
		HealthCheckPeriod: c.DBHealthCheckPeriod,
	}
}

// TelegramBotCfg returns the Telegram bot configuration.
func (c *Config) TelegramBotCfg() TelegramBotConfig {
	return TelegramBotConfig{
		Token:        c.BotToken,
		Username:     c.TelegramBotUsername,
		AdminIDs:     c.AdminIDs,
		TargetChatID: c.TargetChatID,
	}
}

// TelegramMTProtoCfg returns the Telegram MTProto configuration.
func (c *Config) TelegramMTProtoCfg() TelegramMTProtoConfig {
	return TelegramMTProtoConfig{
		APIID:       c.TGAPIID,
		APIHash:     c.TGAPIHash,
		Phone:       c.TGPhone,
		Password2FA: c.TG2FAPassword,
		SessionPath: c.TGSessionPath,
	}
}

// LLMCfg returns the LLM provider configuration.
func (c *Config) LLMCfg() LLMConfig {
	return LLMConfig{
		APIKey:             c.LLMAPIKey,
		Model:              c.LLMModel,
		AnthropicAPIKey:    c.AnthropicAPIKey,
		GoogleAPIKey:       c.GoogleAPIKey,
		GoogleAPIKeyPaid:   c.GoogleAPIKeyPaid,
		CohereAPIKey:       c.CohereAPIKey,
		OpenRouterAPIKey:   c.OpenRouterAPIKey,
		CircuitThreshold:   c.LLMCircuitThreshold,
		CircuitTimeout:     c.LLMCircuitTimeout,
		SummarizeModel:     c.LLMSummarizeModel,
		ClusterModel:       c.LLMClusterModel,
		NarrativeModel:     c.LLMNarrativeModel,
		TranslateModel:     c.LLMTranslateModel,
		CompleteModel:      c.LLMCompleteModel,
		RelevanceGateModel: c.LLMRelevanceGateModel,
		CompressModel:      c.LLMCompressModel,
		TranslationModel:   c.TranslationModel,
	}
}

// EmbeddingCfg returns the embedding provider configuration.
func (c *Config) EmbeddingCfg() EmbeddingConfig {
	return EmbeddingConfig{
		OpenAIModel:      c.OpenAIEmbeddingModel,
		OpenAIDimensions: c.OpenAIEmbeddingDimensions,
		CohereModel:      c.CohereEmbeddingModel,
		ProviderOrder:    c.EmbeddingProviderOrder,
		CircuitThreshold: c.EmbeddingCircuitThreshold,
		CircuitTimeout:   c.EmbeddingCircuitTimeout,
	}
}

// DigestCfg returns the digest generation configuration.
func (c *Config) DigestCfg() DigestConfig {
	return DigestConfig{
		Window:               c.DigestWindow,
		TopN:                 c.DigestTopN,
		TickInterval:         c.SchedulerTickInterval,
		CatchupWindow:        c.SchedulerCatchupWindow,
		TimeToAlertThreshold: c.TimeToDigestAlertThreshold,
	}
}

// ClusterCfg returns the clustering configuration.
func (c *Config) ClusterCfg() ClusterConfig {
	return ClusterConfig{
		SimilarityThreshold:  c.ClusterSimilarityThreshold,
		CoherenceThreshold:   c.ClusterCoherenceThreshold,
		TimeWindowHours:      c.ClusterTimeWindowHours,
		CrossTopicEnabled:    c.CrossTopicClusteringEnabled,
		CrossTopicSimilarity: c.CrossTopicSimilarityThreshold,
		EvidenceEnabled:      c.EvidenceClusteringEnabled,
		EvidenceBoost:        c.EvidenceClusteringBoost,
		EvidenceMinScore:     c.EvidenceClusteringMinScore,
	}
}

// EnrichmentCfg returns the source enrichment configuration.
func (c *Config) EnrichmentCfg() EnrichmentConfig {
	return EnrichmentConfig{
		Enabled:            c.EnrichmentEnabled,
		MaxResults:         c.EnrichmentMaxResults,
		CacheTTLHours:      c.EnrichmentCacheTTLHours,
		QueueMax:           c.EnrichmentQueueMax,
		MinAgreement:       c.EnrichmentMinAgreement,
		MaxEvidencePerItem: c.EnrichmentMaxEvidenceItem,
		AllowlistDomains:   c.EnrichmentAllowlistDomains,
		DenylistDomains:    c.EnrichmentDenylistDomains,
		DedupSimilarity:    c.EnrichmentDedupSimilarity,
		MaxSeconds:         c.EnrichmentMaxSeconds,
		Providers:          c.EnrichmentProviders,
		ProviderCooldown:   c.EnrichmentProviderCooldown,
		ProviderGrace:      c.EnrichmentProviderGrace,
		QueryTranslate:     c.EnrichmentQueryTranslate,
		QueryLLM:           c.EnrichmentQueryLLM,
		QueryLLMModel:      c.EnrichmentQueryLLMModel,
		MaxQueriesPerItem:  c.EnrichmentMaxQueriesPerItem,
		LanguagePolicy:     c.EnrichmentLanguagePolicy,
		LLMTimeout:         c.EnrichmentLLMTimeout,
		DailyBudgetUSD:     c.EnrichmentDailyBudgetUSD,
		MonthlyCapUSD:      c.EnrichmentMonthlyCapUSD,
		EventRegistryRPM:   c.EnrichmentEventRegistryRPM,
		NewsAPIRPM:         c.EnrichmentNewsAPIRPM,
		DailyLimit:         c.EnrichmentDailyLimit,
		MonthlyLimit:       c.EnrichmentMonthlyLimit,
	}
}

// ExpandedViewCfg returns the expanded view configuration.
func (c *Config) ExpandedViewCfg() ExpandedViewConfig {
	return ExpandedViewConfig{
		Enabled:           c.ExpandedViewEnabled,
		BaseURL:           c.ExpandedViewBaseURL,
		SigningSecret:     c.ExpandedViewSigningSecret,
		TTLHours:          c.ExpandedViewTTLHours,
		RequireAdmin:      c.ExpandedViewRequireAdmin,
		AllowSystemTokens: c.ExpandedViewAllowSystemTokens,
		ShortcutEnabled:   c.ExpandedShortcutEnabled,
		ShortcutName:      c.ExpandedShortcutName,
		ShortcutICloudURL: c.ExpandedShortcutICloudURL,
		ShortcutMaxChars:  c.ExpandedShortcutMaxChars,
	}
}

// CrawlerCfg returns the web crawler configuration.
func (c *Config) CrawlerCfg() CrawlerConfig {
	return CrawlerConfig{
		Depth:           c.CrawlDepth,
		RateLimitRPS:    c.CrawlRateLimitRPS,
		BatchSize:       c.CrawlBatchSize,
		ClaimTTL:        c.CrawlClaimTTL,
		UserAgent:       c.CrawlUserAgent,
		SeedsFile:       c.CrawlSeedsFile,
		QueueMaxPending: c.CrawlerQueueMaxPending,
	}
}

// YaCyCfg returns the YaCy provider configuration.
func (c *Config) YaCyCfg() ProviderYaCyConfig {
	return ProviderYaCyConfig{
		Enabled:    c.YaCyEnabled,
		BaseURL:    c.YaCyBaseURL,
		Timeout:    c.YaCyTimeout,
		User:       c.YaCyUser,
		Password:   c.YaCyPassword,
		Resource:   c.YaCyResource,
		MaxResults: c.YaCyMaxResults,
	}
}

// SolrCfg returns the Solr provider configuration.
func (c *Config) SolrCfg() ProviderSolrConfig {
	return ProviderSolrConfig{
		Enabled:    c.SolrEnabled,
		BaseURL:    c.SolrBaseURL,
		Timeout:    c.SolrTimeout,
		MaxResults: c.SolrMaxResults,
	}
}
