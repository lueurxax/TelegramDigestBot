package config

import (
	"os"
	"testing"
)

// Test environment variable keys.
const (
	testEnvPostgresDSN  = "POSTGRES_DSN"
	testEnvBotToken     = "BOT_TOKEN"
	testEnvTargetChatID = "TARGET_CHAT_ID"
	testEnvTGAPIID      = "TG_API_ID"
	testEnvTGAPIHash    = "TG_API_HASH"
	//nolint:gosec // Test env key name, not a credential.
	testEnvLLMAPIKey = "LLM_API_KEY"
	testEnvAdminIDs     = "ADMIN_IDS"
)

// Test values.
const (
	testPostgresDSN  = "postgres://localhost/test"
	testBotToken     = "123456:ABC-DEF"
	testTargetChatID = "-1001234567890"
	testTGAPIID      = "12345"
	testTGAPIHash    = "abcdef123456"
	testLLMAPIKey    = "sk-test-key"
	testErrLoad      = "Load() error = %v"
	testDefaultEnv   = "local"
	testDefaultModel = "gpt-4o-mini"
	testDefaultWindow = "60m"
	testDefaultSessionPath = "./tg.session"
)

func setRequiredEnvVars(t *testing.T) {
	t.Helper()

	t.Setenv(testEnvPostgresDSN, testPostgresDSN)
	t.Setenv(testEnvBotToken, testBotToken)
	t.Setenv(testEnvTargetChatID, testTargetChatID)
	t.Setenv(testEnvTGAPIID, testTGAPIID)
	t.Setenv(testEnvTGAPIHash, testTGAPIHash)
	t.Setenv(testEnvLLMAPIKey, testLLMAPIKey)
}

func TestLoad_MissingRequired(t *testing.T) {
	// Clear all required vars
	os.Unsetenv(testEnvPostgresDSN)
	os.Unsetenv(testEnvBotToken)
	os.Unsetenv(testEnvTargetChatID)
	os.Unsetenv(testEnvTGAPIID)
	os.Unsetenv(testEnvTGAPIHash)
	os.Unsetenv(testEnvLLMAPIKey)

	_, err := Load()
	if err == nil {
		t.Error("expected error for missing required env vars")
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	setRequiredEnvVars(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf(testErrLoad, err)
	}

	if cfg.PostgresDSN != testPostgresDSN {
		t.Errorf("PostgresDSN = %q, want %q", cfg.PostgresDSN, testPostgresDSN)
	}

	if cfg.BotToken != testBotToken {
		t.Errorf("BotToken = %q, want %q", cfg.BotToken, testBotToken)
	}

	if cfg.TargetChatID != -1001234567890 {
		t.Errorf("TargetChatID = %d, want %d", cfg.TargetChatID, -1001234567890)
	}

	if cfg.TGAPIID != 12345 {
		t.Errorf("TGAPIID = %d, want %d", cfg.TGAPIID, 12345)
	}
}

func TestLoad_Defaults(t *testing.T) {
	setRequiredEnvVars(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf(testErrLoad, err)
	}

	if cfg.AppEnv != testDefaultEnv {
		t.Errorf("AppEnv default = %q, want %q", cfg.AppEnv, testDefaultEnv)
	}

	if cfg.LLMModel != testDefaultModel {
		t.Errorf("LLMModel default = %q, want %q", cfg.LLMModel, testDefaultModel)
	}

	if cfg.DigestWindow != testDefaultWindow {
		t.Errorf("DigestWindow default = %q, want %q", cfg.DigestWindow, testDefaultWindow)
	}

	if cfg.DigestTopN != 20 {
		t.Errorf("DigestTopN default = %d, want %d", cfg.DigestTopN, 20)
	}

	if cfg.HealthPort != 8080 {
		t.Errorf("HealthPort default = %d, want %d", cfg.HealthPort, 8080)
	}

	if !cfg.LeaderElectionEnabled {
		t.Error("LeaderElectionEnabled should default to true")
	}

	if cfg.TGSessionPath != testDefaultSessionPath {
		t.Errorf("TGSessionPath default = %q, want %q", cfg.TGSessionPath, testDefaultSessionPath)
	}
}

func TestLoad_AdminIDs(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv(testEnvAdminIDs, "111,222,333")

	cfg, err := Load()
	if err != nil {
		t.Fatalf(testErrLoad, err)
	}

	if len(cfg.AdminIDs) != 3 {
		t.Fatalf("AdminIDs length = %d, want %d", len(cfg.AdminIDs), 3)
	}

	expected := []int64{111, 222, 333}
	for i, want := range expected {
		if cfg.AdminIDs[i] != want {
			t.Errorf("AdminIDs[%d] = %d, want %d", i, cfg.AdminIDs[i], want)
		}
	}
}

func TestLoad_InvalidNumeric(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv(testEnvTargetChatID, "not-a-number")

	_, err := Load()
	if err == nil {
		t.Error("expected error for invalid TARGET_CHAT_ID")
	}
}
