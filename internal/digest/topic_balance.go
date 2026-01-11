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

	maxPerTopic := int(math.Floor(float64(capFraction) * float64(targetN)))
	if maxPerTopic < 1 {
		maxPerTopic = 1
	}
	if minTopics < 0 {
		minTopics = 0
	}
	if minTopics > targetN {
		minTopics = targetN
	}

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

	topicsAvailable := len(topics)
	selectedIndices := make(map[int]struct{})
	topicCounts := make(map[string]int)

	for i := 0; i < minTopics && i < len(topics); i++ {
		idx := topics[i].index
		key, _ := topicKey(items[idx].Topic)
		selectedIndices[idx] = struct{}{}
		topicCounts[key]++
	}

	for idx, item := range items {
		if len(selectedIndices) >= targetN {
			break
		}
		if _, ok := selectedIndices[idx]; ok {
			continue
		}
		key, _ := topicKey(item.Topic)
		if topicCounts[key] >= maxPerTopic {
			continue
		}
		selectedIndices[idx] = struct{}{}
		topicCounts[key]++
	}

	relaxed := false
	if len(selectedIndices) < targetN {
		relaxed = true
		for idx, item := range items {
			if len(selectedIndices) >= targetN {
				break
			}
			if _, ok := selectedIndices[idx]; ok {
				continue
			}
			key, _ := topicKey(item.Topic)
			selectedIndices[idx] = struct{}{}
			topicCounts[key]++
		}
	}

	selected := make([]db.Item, 0, targetN)
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
