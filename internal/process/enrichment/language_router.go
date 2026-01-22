package enrichment

import (
	"context"
	"strings"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

type historyRepository interface {
	GetRecentMessagesForChannel(ctx context.Context, channelID string, before time.Time, limit int) ([]string, error)
}

type LanguageRouter struct {
	policy domain.LanguageRoutingPolicy
	db     historyRepository
}

func NewLanguageRouter(policy domain.LanguageRoutingPolicy, db historyRepository) *LanguageRouter {
	return &LanguageRouter{
		policy: policy,
		db:     db,
	}
}

const historyLimit = 10

// GetTargetLanguages determines target languages for enrichment based on item context.
func (r *LanguageRouter) GetTargetLanguages(ctx context.Context, item *db.EnrichmentQueueItem) []string {
	// 1. Direct Channel username match (highest priority)
	if langs := r.matchChannelUsername(item.ChannelUsername); langs != nil {
		return langs
	}

	// 2. Keyword match in Channel Metadata (Highest source confidence)
	if langs := r.matchContextKeywords(item.ChannelTitle, item.ChannelDescription); len(langs) > 0 {
		return langs
	}

	// 3. Topic match (Explicit policy)
	if langs := r.matchTopic(item.Topic); langs != nil {
		return langs
	}

	// 4. Keyword match in Item Content (Medium source confidence)
	if langs := r.matchContextKeywords(item.Summary, item.Topic); len(langs) > 0 {
		return langs
	}

	// 5. Keyword match in History (Lowest source confidence)
	if langs := r.matchHistory(ctx, item.ChannelID); langs != nil {
		return langs
	}

	return r.defaultLanguages()
}

func (r *LanguageRouter) matchChannelUsername(username string) []string {
	if username == "" {
		return nil
	}

	langs, ok := r.policy.Channel["@"+strings.TrimPrefix(username, "@")]
	if !ok {
		return nil
	}

	return langs
}

func (r *LanguageRouter) matchTopic(topic string) []string {
	if topic == "" {
		return nil
	}

	langs, ok := r.policy.Topic[topic]
	if !ok {
		return nil
	}

	return langs
}

func (r *LanguageRouter) matchHistory(ctx context.Context, channelID string) []string {
	if channelID == "" {
		return nil
	}

	history, err := r.db.GetRecentMessagesForChannel(ctx, channelID, time.Now(), historyLimit)
	if err != nil {
		return nil
	}

	for _, msg := range history {
		if langs := r.matchContextKeywords(msg); len(langs) > 0 {
			return langs
		}
	}

	return nil
}

func (r *LanguageRouter) defaultLanguages() []string {
	if len(r.policy.Default) > 0 {
		return r.policy.Default
	}

	return []string{"en"}
}

func (r *LanguageRouter) matchContextKeywords(texts ...string) []string {
	for _, cp := range r.policy.Context {
		for _, text := range texts {
			if r.matchesKeywords(text, cp.Keywords) {
				return cp.Languages
			}
		}
	}

	return nil
}

func (r *LanguageRouter) matchesKeywords(text string, keywords []string) bool {
	if text == "" {
		return false
	}

	lowerText := strings.ToLower(text)

	for _, kw := range keywords {
		if strings.Contains(lowerText, strings.ToLower(kw)) {
			return true
		}
	}

	return false
}
