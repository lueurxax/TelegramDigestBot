package config

import (
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv                  string  `env:"APP_ENV" envDefault:"local"`
	PostgresDSN             string  `env:"POSTGRES_DSN,required"`
	BotToken                string  `env:"BOT_TOKEN,required"`
	AdminIDs                []int64 `env:"ADMIN_IDS" envSeparator:","`
	TargetChatID            int64   `env:"TARGET_CHAT_ID,required"`
	TGAPIID                 int     `env:"TG_API_ID,required"`
	TGAPIHash               string  `env:"TG_API_HASH,required"`
	TGPhone                 string  `env:"TG_PHONE"`
	TG2FAPassword           string  `env:"TG_2FA_PASSWORD"`
	TGSessionPath           string  `env:"TG_SESSION_PATH" envDefault:"./tg.session"`
	LLMAPIKey               string  `env:"LLM_API_KEY,required"`
	LLMModel                string  `env:"LLM_MODEL" envDefault:"gpt-4o-mini"`
	DigestWindow            string  `env:"DIGEST_WINDOW" envDefault:"60m"`
	DigestTopN              int     `env:"DIGEST_TOP_N" envDefault:"20"`
	RelevanceThreshold      float32 `env:"RELEVANCE_THRESHOLD" envDefault:"0.5"`
	ImportanceThreshold     float32 `env:"IMPORTANCE_THRESHOLD" envDefault:"0.3"`
	SimilarityThreshold     float32 `env:"SIMILARITY_THRESHOLD" envDefault:"0.65"`
	RateLimitRPS            int     `env:"RATE_LIMIT_RPS" envDefault:"1"`
	HealthPort              int     `env:"HEALTH_PORT" envDefault:"8080"`
	LeaderElectionEnabled   bool    `env:"LEADER_ELECTION_ENABLED" envDefault:"true"`
	LeaderElectionLeaseName string  `env:"LEADER_ELECTION_LEASE_NAME" envDefault:"digest-scheduler-lease"`
	ReaderFetchLimit        int     `env:"READER_FETCH_LIMIT" envDefault:"20"`
	WorkerBatchSize         int     `env:"WORKER_BATCH_SIZE" envDefault:"10"`
	WorkerPollInterval      string  `env:"WORKER_POLL_INTERVAL" envDefault:"10s"`
	SchedulerTickInterval   string  `env:"SCHEDULER_TICK_INTERVAL" envDefault:"10m"`
	SchedulerCatchupWindow  string  `env:"SCHEDULER_CATCHUP_WINDOW" envDefault:"24h"`
	RelevanceGateEnabled    bool    `env:"RELEVANCE_GATE_ENABLED" envDefault:"false"`
	TopicDiversityCap       float32 `env:"TOPIC_DIVERSITY_CAP" envDefault:"0.30"`
	FreshnessDecayHours     int     `env:"FRESHNESS_DECAY_HOURS" envDefault:"36"`
	FreshnessFloor          float32 `env:"FRESHNESS_FLOOR" envDefault:"0.4"`
	MinTopicCount           int     `env:"MIN_TOPIC_COUNT" envDefault:"3"`
	RatingMinSampleChannel  int     `env:"RATING_MIN_SAMPLE_CHANNEL" envDefault:"15"`
	RatingMinSampleGlobal   int     `env:"RATING_MIN_SAMPLE_GLOBAL" envDefault:"100"`

	// Link enrichment
	LinkEnrichmentEnabled bool          `env:"LINK_ENRICHMENT_ENABLED" envDefault:"false"`
	WebFetchRPS           float64       `env:"WEB_FETCH_RPS" envDefault:"2"`
	WebFetchTimeout       time.Duration `env:"WEB_FETCH_TIMEOUT" envDefault:"30s"`
	LinkCacheTTL          time.Duration `env:"LINK_CACHE_TTL" envDefault:"24h"`
	TelegramLinkCacheTTL  time.Duration `env:"TG_LINK_CACHE_TTL" envDefault:"1h"`
	MaxLinksPerMessage    int           `env:"MAX_LINKS_PER_MESSAGE" envDefault:"3"`
	MaxContentLength      int           `env:"MAX_CONTENT_LENGTH" envDefault:"5000"`
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
