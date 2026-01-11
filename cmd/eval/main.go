package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
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

func main() {
	inputPath := flag.String("input", "docs/eval/sample.jsonl", "Path to JSONL dataset")
	relevanceThreshold := flag.Float64("relevance-threshold", 0.5, "Relevance score threshold")
	importanceThreshold := flag.Float64("importance-threshold", 0.3, "Importance score threshold")
	ignoreImportance := flag.Bool("ignore-importance", false, "Ignore importance score threshold")
	minPrecision := flag.Float64("min-precision", -1, "Fail if precision is below this value (disabled if <0)")
	maxNoiseRate := flag.Float64("max-noise-rate", -1, "Fail if noise rate is above this value (disabled if <0)")
	flag.Parse()

	f, err := os.Open(*inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open input: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	stats := evalStats{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var rec evalRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			stats.skipped++
			continue
		}

		label := normalizeLabel(rec.Label, rec.Rating)
		if label == "" {
			stats.skipped++
			continue
		}

		stats.total++
		switch label {
		case "good":
			stats.goodCount++
		case "bad":
			stats.badCount++
		case "irrelevant":
			stats.irrelevantCount++
		}

		predicted := rec.RelevanceScore >= float32(*relevanceThreshold)
		if !*ignoreImportance {
			predicted = predicted && rec.ImportanceScore >= float32(*importanceThreshold)
		}

		actualPositive := label == "good"
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

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to read input: %v\n", err)
		os.Exit(1)
	}

	printSummary(stats, *relevanceThreshold, *importanceThreshold, *ignoreImportance)

	precision := ratio(stats.tp, stats.tp+stats.fp)
	noiseRate := ratio(stats.fp, stats.tp+stats.fp)
	if *minPrecision >= 0 && precision < *minPrecision {
		fmt.Fprintf(os.Stderr, "precision %.3f is below threshold %.3f\n", precision, *minPrecision)
		os.Exit(1)
	}
	if *maxNoiseRate >= 0 && noiseRate > *maxNoiseRate {
		fmt.Fprintf(os.Stderr, "noise rate %.3f is above threshold %.3f\n", noiseRate, *maxNoiseRate)
		os.Exit(1)
	}
}

func normalizeLabel(label string, rating string) string {
	val := strings.ToLower(strings.TrimSpace(label))
	if val == "" {
		val = strings.ToLower(strings.TrimSpace(rating))
	}
	switch val {
	case "good", "bad", "irrelevant":
		return val
	default:
		return ""
	}
}

func printSummary(stats evalStats, relevanceThreshold, importanceThreshold float64, ignoreImportance bool) {
	predictedTotal := stats.tp + stats.fp
	precision := ratio(stats.tp, predictedTotal)
	recall := ratio(stats.tp, stats.tp+stats.fn)
	noiseRate := ratio(stats.fp, predictedTotal)
	coverage := ratio(predictedTotal, stats.total)

	fmt.Printf("Evaluation Summary\n")
	fmt.Printf("  Records: %d (skipped: %d)\n", stats.total, stats.skipped)
	fmt.Printf("  Labels: good=%d bad=%d irrelevant=%d\n", stats.goodCount, stats.badCount, stats.irrelevantCount)
	if ignoreImportance {
		fmt.Printf("  Thresholds: relevance>=%.2f (importance ignored)\n", relevanceThreshold)
	} else {
		fmt.Printf("  Thresholds: relevance>=%.2f importance>=%.2f\n", relevanceThreshold, importanceThreshold)
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
