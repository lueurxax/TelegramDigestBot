package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lueurxax/telegram-digest-bot/internal/db"
)

const (
	defaultExportPath  = "docs/eval/golden.jsonl"
	defaultExportLimit = 500
	outputDirPerm      = 0o700
	errWriteOutput     = "failed to write output: %v\n"
)

type exportRecord struct {
	ID              string  `json:"id"`
	Label           string  `json:"label"`
	RelevanceScore  float32 `json:"relevance_score"`
	ImportanceScore float32 `json:"importance_score"`
}

func main() {
	outPath := flag.String("out", defaultExportPath, "Output JSONL path")
	limit := flag.Int("limit", defaultExportLimit, "Max labeled items to export")
	dsn := flag.String("dsn", os.Getenv("POSTGRES_DSN"), "Postgres DSN")

	flag.Parse()

	if *dsn == "" {
		fmt.Fprintln(os.Stderr, "POSTGRES_DSN is required (or provide -dsn).")
		os.Exit(1)
	}

	if *limit <= 0 {
		fmt.Fprintln(os.Stderr, "limit must be positive")
		os.Exit(1)
	}

	ctx := context.Background()

	database, err := db.New(ctx, *dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	records, err := database.GetLabeledAnnotations(ctx, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load labeled annotations: %v\n", err)
		os.Exit(1)
	}

	cleanPath := filepath.Clean(*outPath)
	if err := os.MkdirAll(filepath.Dir(cleanPath), outputDirPerm); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output directory: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Create(cleanPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output file: %v\n", err)
		os.Exit(1)
	}

	defer func() {
		_ = f.Close()
	}()

	writer := bufio.NewWriter(f)

	for _, rec := range records {
		out := exportRecord{
			ID:              rec.ID,
			Label:           rec.Label,
			RelevanceScore:  rec.RelevanceScore,
			ImportanceScore: rec.ImportanceScore,
		}

		line, err := json.Marshal(out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to encode record: %v\n", err)
			os.Exit(1)
		}

		if _, err := writer.Write(line); err != nil {
			fmt.Fprintf(os.Stderr, errWriteOutput, err)
			os.Exit(1)
		}

		if err := writer.WriteByte('\n'); err != nil {
			fmt.Fprintf(os.Stderr, errWriteOutput, err)
			os.Exit(1)
		}
	}

	if err := writer.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to flush output: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Exported %d labeled items to %s\n", len(records), cleanPath)
}
