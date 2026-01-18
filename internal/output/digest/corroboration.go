package digest

import (
	"fmt"
	"html"
	"strings"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	maxCorroborationChannels = 3
	labelFalse               = "false"
	labelTrue                = "true"
)

func (s *Scheduler) buildCorroborationLine(items []db.Item, representative db.Item) string {
	if len(items) <= 1 {
		observability.CorroborationCoverage.WithLabelValues(labelFalse).Inc()

		return ""
	}

	repKey := channelKey(representative)
	channels, relatedLink := s.collectCorroborationInfo(items, representative, repKey)

	if len(channels) > 0 {
		observability.CorroborationCoverage.WithLabelValues(labelTrue).Inc()

		if len(channels) > maxCorroborationChannels {
			channels = channels[:maxCorroborationChannels]
		}

		return fmt.Sprintf("\n    ↳ <i>Also reported by: %s</i>", strings.Join(channels, ", "))
	}

	observability.CorroborationCoverage.WithLabelValues(labelFalse).Inc()

	if relatedLink != "" {
		return fmt.Sprintf("\n    ↳ <i>Related: %s</i>", relatedLink)
	}

	return ""
}

func (s *Scheduler) collectCorroborationInfo(items []db.Item, representative db.Item, repKey string) ([]string, string) {
	seen := make(map[string]bool)
	channels := make([]string, 0, len(items))

	var relatedLink string

	for _, item := range items {
		key := channelKey(item)
		if key == "" {
			continue
		}

		if repKey != "" && key == repKey {
			if item.SourceMsgID != representative.SourceMsgID && relatedLink == "" {
				relatedLink = s.formatLink(item, "Related")
			}

			continue
		}

		label := channelLabel(item)
		if label == "" || seen[label] {
			continue
		}

		label = html.EscapeString(label)
		seen[label] = true
		channels = append(channels, label)
	}

	return channels, relatedLink
}

func channelKey(item db.Item) string {
	if item.SourceChannel != "" {
		return "u:" + strings.ToLower(item.SourceChannel)
	}

	if item.SourceChannelID != 0 {
		return fmt.Sprintf("id:%d", item.SourceChannelID)
	}

	if item.SourceChannelTitle != "" {
		return "t:" + strings.ToLower(item.SourceChannelTitle)
	}

	return ""
}

func channelLabel(item db.Item) string {
	if item.SourceChannel != "" {
		return "@" + item.SourceChannel
	}

	if item.SourceChannelTitle != "" {
		return item.SourceChannelTitle
	}

	return ""
}
