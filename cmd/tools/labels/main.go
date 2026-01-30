package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	defaultExportPath  = "docs/eval/golden.jsonl"
	defaultExportLimit = 500
	outputDirPerm      = 0o700
	errFmt             = "%v\n"
)

var (
	errDSNRequired    = errors.New("POSTGRES_DSN is required (or provide -dsn)")
	errLimitMustBePos = errors.New("limit must be positive")
)

type exportConfig struct {
	outPath string
	limit   int
	dsn     string
}

type exportRecord struct {
	ID              string  `json:"id"`
	Label           string  `json:"label"`
	RelevanceScore  float32 `json:"relevance_score"`
	ImportanceScore float32 `json:"importance_score"`
}

func main() {
	cfg := parseFlags()

	if err := validateConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, errFmt, err)
		os.Exit(1)
	}

	if err := runExport(cfg); err != nil {
		fmt.Fprintf(os.Stderr, errFmt, err)
		os.Exit(1)
	}
}

func parseFlags() exportConfig {
	cfg := exportConfig{}

	flag.StringVar(&cfg.outPath, "out", defaultExportPath, "Output JSONL path")
	flag.IntVar(&cfg.limit, "limit", defaultExportLimit, "Max labeled items to export")
	flag.StringVar(&cfg.dsn, "dsn", os.Getenv("POSTGRES_DSN"), "Postgres DSN")

	flag.Parse()

	return cfg
}

func validateConfig(cfg exportConfig) error {
	if cfg.dsn == "" {
		return errDSNRequired
	}

	if cfg.limit <= 0 {
		return errLimitMustBePos
	}

	return nil
}

func runExport(cfg exportConfig) error {
	ctx := context.Background()
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	database, err := db.New(ctx, cfg.dsn, &logger)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer database.Close()

	records, err := database.GetLabeledAnnotations(ctx, cfg.limit)
	if err != nil {
		return fmt.Errorf("failed to load labeled annotations: %w", err)
	}

	if err := writeRecords(records, cfg.outPath); err != nil {
		return err
	}

	logger.Info().Int("count", len(records)).Str("path", cfg.outPath).Msg("Exported labeled items")

	return nil
}

func writeRecords(records []db.AnnotationExport, outPath string) error {
	cleanPath := filepath.Clean(outPath)

	if err := os.MkdirAll(filepath.Dir(cleanPath), outputDirPerm); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	f, err := os.Create(cleanPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}

	defer func() {
		_ = f.Close()
	}()

	writer := bufio.NewWriter(f)

	for _, rec := range records {
		if err := writeRecord(writer, rec); err != nil {
			return err
		}
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush output: %w", err)
	}

	return nil
}

func writeRecord(writer *bufio.Writer, rec db.AnnotationExport) error {
	out := exportRecord{
		ID:              rec.ID,
		Label:           rec.Label,
		RelevanceScore:  rec.RelevanceScore,
		ImportanceScore: rec.ImportanceScore,
	}

	line, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("failed to encode record: %w", err)
	}

	if _, err := writer.Write(line); err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	if err := writer.WriteByte('\n'); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	return nil
}
