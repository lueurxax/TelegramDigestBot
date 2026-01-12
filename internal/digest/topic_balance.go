package digest

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/db"
)

const unknownTopicKey = "__unknown__"

type topicBalanceResult struct {
	Items           []db.Item
	TopicsSelected  int
	TopicsAvailable int
	MaxPerTopic     int
	Relaxed         bool
}

type topicCandidate struct {
	key   string
	index int
}

func applyFreshnessDecay(score float32, tgDate time.Time, decayHours int, floor float32) float32 {
	if decayHours <= 0 {
		return score
	}

	if floor < 0 {
		floor = 0
	} else if floor > 1 {
		floor = 1
	}

	hoursOld := time.Since(tgDate).Hours()
	if hoursOld < 0 {
		hoursOld = 0
	}

	decay := math.Exp(-hoursOld / float64(decayHours))

	if decay < float64(floor) {
		decay = float64(floor)
	}

	return score * float32(decay)
}

func applyTopicBalance(items []db.Item, topN int, capFraction float32, minTopics int) topicBalanceResult {
	if topN <= 0 || len(items) == 0 {
		return topicBalanceResult{Items: nil}
	}

	targetN := min(topN, len(items))

	if capFraction <= 0 || capFraction >= 1 {
		return selectInitialTopN(items, targetN)
	}

	maxPerTopic := calculateMaxPerTopic(targetN, capFraction)
	minTopics = clampMinTopics(minTopics, targetN)

	topics := getTopicCandidates(items)

	selectedIndices := make(map[int]struct{})
	topicCounts := make(map[string]int)

	// Phase 1: Ensure minTopics diversity
	for i := 0; i < minTopics && i < len(topics); i++ {
		idx := topics[i].index
		key, _ := topicKey(items[idx].Topic)

		selectedIndices[idx] = struct{}{}
		topicCounts[key]++
	}

	// Phase 2: Fill remaining slots respecting maxPerTopic
	fillRemainingSlots(items, selectedIndices, topicCounts, targetN, maxPerTopic)

	relaxed := false
	if len(selectedIndices) < targetN {
		relaxed = true

		fillRemainingSlots(items, selectedIndices, topicCounts, targetN, -1) // -1 means no cap
	}

	return buildTopicBalanceResult(items, selectedIndices, relaxed, len(topics), maxPerTopic)
}

func selectInitialTopN(items []db.Item, targetN int) topicBalanceResult {
	selected := items
	if len(selected) > targetN {
		selected = selected[:targetN]
	}

	return topicBalanceResult{
		Items:           selected,
		TopicsSelected:  countDistinctTopics(selected),
		TopicsAvailable: countDistinctTopics(items),
	}
}

func calculateMaxPerTopic(targetN int, capFraction float32) int {
	maxPerTopic := int(math.Floor(float64(capFraction) * float64(targetN)))
	if maxPerTopic < 1 {
		maxPerTopic = 1
	}

	return maxPerTopic
}

func clampMinTopics(minTopics, targetN int) int {
	if minTopics < 0 {
		minTopics = 0
	}

	if minTopics > targetN {
		minTopics = targetN
	}

	return minTopics
}

func getTopicCandidates(items []db.Item) []topicCandidate {
	topicFirstIndex := make(map[string]int)
	topicEligible := make(map[string]bool)

	for idx, item := range items {
		key, eligible := topicKey(item.Topic)

		if _, ok := topicFirstIndex[key]; !ok {
			topicFirstIndex[key] = idx
			if eligible {
				topicEligible[key] = true
			}
		}
	}

	var topics []topicCandidate

	for key, idx := range topicFirstIndex {
		if topicEligible[key] {
			topics = append(topics, topicCandidate{key: key, index: idx})
		}
	}

	sort.Slice(topics, func(i, j int) bool { return topics[i].index < topics[j].index })

	return topics
}

func fillRemainingSlots(items []db.Item, selectedIndices map[int]struct{}, topicCounts map[string]int, targetN, maxPerTopic int) {
	for idx, item := range items {
		if len(selectedIndices) >= targetN {
			break
		}

		if _, ok := selectedIndices[idx]; ok {
			continue
		}

		key, _ := topicKey(item.Topic)
		if maxPerTopic != -1 && topicCounts[key] >= maxPerTopic {
			continue
		}

		selectedIndices[idx] = struct{}{}
		topicCounts[key]++
	}
}

func buildTopicBalanceResult(items []db.Item, selectedIndices map[int]struct{}, relaxed bool, topicsAvailable, maxPerTopic int) topicBalanceResult {
	selected := make([]db.Item, 0, len(selectedIndices))
	for idx, item := range items {
		if _, ok := selectedIndices[idx]; ok {
			selected = append(selected, item)
		}
	}

	return topicBalanceResult{
		Items:           selected,
		TopicsSelected:  countDistinctTopics(selected),
		TopicsAvailable: topicsAvailable,
		MaxPerTopic:     maxPerTopic,
		Relaxed:         relaxed,
	}
}

func countDistinctTopics(items []db.Item) int {
	seen := make(map[string]struct{})

	for _, item := range items {
		key, eligible := topicKey(item.Topic)

		if !eligible {
			continue
		}

		seen[key] = struct{}{}
	}

	return len(seen)
}

func topicKey(topic string) (string, bool) {
	normalized := strings.TrimSpace(strings.ToLower(topic))

	if normalized == "" {
		return unknownTopicKey, false
	}

	return normalized, true
}

func min(a, b int) int {
	if a < b {
		return a
	}

	return b
}
