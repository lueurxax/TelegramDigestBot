// Package main provides evaluation tools for measuring digest quality.
//
// The eval tool compares labeled data (golden set) against system outputs
// to calculate precision, recall, and noise rate metrics.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

const (
	labelGood       = "good"
	labelBad        = "bad"
	labelIrrelevant = "irrelevant"

	defaultRelevanceThreshold  = 0.5
	defaultImportanceThreshold = 0.3
	maxScannerBufferSize       = 1024
	scannerBufferMultiplier    = 64

	errFmt = "%v\n"
)

var (
	errPrecisionBelowThreshold = errors.New("precision below threshold")
	errNoiseRateAboveThreshold = errors.New("noise rate above threshold")
)

type evalRecord struct {
	ID              string  `json:"id"`
	Label           string  `json:"label"`
	Rating          string  `json:"rating"`
	RelevanceScore  float32 `json:"relevance_score"`
	ImportanceScore float32 `json:"importance_score"`
}

type evalStats struct {
	total           int
	skipped         int
	tp              int
	fp              int
	fn              int
	tn              int
	goodCount       int
	badCount        int
	irrelevantCount int
}

type evalConfig struct {
	inputPath           string
	relevanceThreshold  float64
	importanceThreshold float64
	ignoreImportance    bool
	minPrecision        float64
	maxNoiseRate        float64
}

func main() {
	cfg := parseFlags()

	stats, err := processInputFile(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, errFmt, err)
		os.Exit(1)
	}

	printSummary(stats, cfg)

	if err := checkThresholds(stats, cfg); err != nil {
		fmt.Fprintf(os.Stderr, errFmt, err)
		os.Exit(1)
	}
}

func parseFlags() evalConfig {
	cfg := evalConfig{}

	flag.StringVar(&cfg.inputPath, "input", "docs/eval/sample.jsonl", "Path to JSONL dataset")
	flag.Float64Var(&cfg.relevanceThreshold, "relevance-threshold", defaultRelevanceThreshold, "Relevance score threshold")
	flag.Float64Var(&cfg.importanceThreshold, "importance-threshold", defaultImportanceThreshold, "Importance score threshold")
	flag.BoolVar(&cfg.ignoreImportance, "ignore-importance", false, "Ignore importance score threshold")
	flag.Float64Var(&cfg.minPrecision, "min-precision", -1, "Fail if precision is below this value (disabled if <0)")
	flag.Float64Var(&cfg.maxNoiseRate, "max-noise-rate", -1, "Fail if noise rate is above this value (disabled if <0)")

	flag.Parse()

	return cfg
}

func processInputFile(cfg evalConfig) (evalStats, error) {
	f, err := os.Open(cfg.inputPath)
	if err != nil {
		return evalStats{}, fmt.Errorf("failed to open input: %w", err)
	}
	defer f.Close()

	return scanRecords(f, cfg)
}

func scanRecords(f *os.File, cfg evalConfig) (evalStats, error) {
	stats := evalStats{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, scannerBufferMultiplier*maxScannerBufferSize), maxScannerBufferSize*maxScannerBufferSize)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		processLine(line, cfg, &stats)
	}

	if err := scanner.Err(); err != nil {
		return stats, fmt.Errorf("failed to read input: %w", err)
	}

	return stats, nil
}

func processLine(line string, cfg evalConfig, stats *evalStats) {
	var rec evalRecord
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		stats.skipped++

		return
	}

	label := normalizeLabel(rec.Label, rec.Rating)
	if label == "" {
		stats.skipped++

		return
	}

	stats.total++
	updateLabelCounts(label, stats)
	updateConfusionMatrix(rec, label, cfg, stats)
}

func updateLabelCounts(label string, stats *evalStats) {
	switch label {
	case labelGood:
		stats.goodCount++
	case labelBad:
		stats.badCount++
	case labelIrrelevant:
		stats.irrelevantCount++
	}
}

func updateConfusionMatrix(rec evalRecord, label string, cfg evalConfig, stats *evalStats) {
	predicted := rec.RelevanceScore >= float32(cfg.relevanceThreshold)
	if !cfg.ignoreImportance {
		predicted = predicted && rec.ImportanceScore >= float32(cfg.importanceThreshold)
	}

	actualPositive := label == labelGood

	switch {
	case predicted && actualPositive:
		stats.tp++
	case predicted && !actualPositive:
		stats.fp++
	case !predicted && actualPositive:
		stats.fn++
	default:
		stats.tn++
	}
}

func checkThresholds(stats evalStats, cfg evalConfig) error {
	precision := ratio(stats.tp, stats.tp+stats.fp)
	noiseRate := ratio(stats.fp, stats.tp+stats.fp)

	if cfg.minPrecision >= 0 && precision < cfg.minPrecision {
		return fmt.Errorf("%w: %.3f < %.3f", errPrecisionBelowThreshold, precision, cfg.minPrecision)
	}

	if cfg.maxNoiseRate >= 0 && noiseRate > cfg.maxNoiseRate {
		return fmt.Errorf("%w: %.3f > %.3f", errNoiseRateAboveThreshold, noiseRate, cfg.maxNoiseRate)
	}

	return nil
}

func normalizeLabel(label string, rating string) string {
	val := strings.ToLower(strings.TrimSpace(label))
	if val == "" {
		val = strings.ToLower(strings.TrimSpace(rating))
	}

	switch val {
	case labelGood, labelBad, labelIrrelevant:
		return val
	default:
		return ""
	}
}

func printSummary(stats evalStats, cfg evalConfig) {
	predictedTotal := stats.tp + stats.fp
	precision := ratio(stats.tp, predictedTotal)
	recall := ratio(stats.tp, stats.tp+stats.fn)
	noiseRate := ratio(stats.fp, predictedTotal)
	coverage := ratio(predictedTotal, stats.total)

	fmt.Printf("Evaluation Summary\n")
	fmt.Printf("  Records: %d (skipped: %d)\n", stats.total, stats.skipped)
	fmt.Printf("  Labels: good=%d bad=%d irrelevant=%d\n", stats.goodCount, stats.badCount, stats.irrelevantCount)

	if cfg.ignoreImportance {
		fmt.Printf("  Thresholds: relevance>=%.2f (importance ignored)\n", cfg.relevanceThreshold)
	} else {
		fmt.Printf("  Thresholds: relevance>=%.2f importance>=%.2f\n", cfg.relevanceThreshold, cfg.importanceThreshold)
	}

	fmt.Printf("  Confusion: TP=%d FP=%d FN=%d TN=%d\n", stats.tp, stats.fp, stats.fn, stats.tn)
	fmt.Printf("  Precision: %.3f\n", precision)
	fmt.Printf("  Recall: %.3f\n", recall)
	fmt.Printf("  NoiseRate: %.3f\n", noiseRate)
	fmt.Printf("  Coverage: %.3f\n", coverage)
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}

	return float64(numerator) / float64(denominator)
}
