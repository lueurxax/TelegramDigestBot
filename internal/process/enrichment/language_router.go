package enrichment

import (
	"context"
	"strings"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

type LanguageRouter struct {
	policy domain.LanguageRoutingPolicy
	db     Repository
}

func NewLanguageRouter(policy domain.LanguageRoutingPolicy, db Repository) *LanguageRouter {
	return &LanguageRouter{
		policy: policy,
		db:     db,
	}
}

const historyLimit = 10

// GetTargetLanguages determines target languages for enrichment based on item context.
func (r *LanguageRouter) GetTargetLanguages(ctx context.Context, item *db.EnrichmentQueueItem) []string {
	// 1. Channel username match
	if item.ChannelUsername != "" {
		if langs, ok := r.policy.Channel["@"+strings.TrimPrefix(item.ChannelUsername, "@")]; ok {
			return langs
		}
	}

	// 2. Context keyword match (channel description, title, summary, topic, history)
	detectedContexts := r.detectContexts(ctx, item)

	for _, ctxPolicy := range r.policy.Context {
		for _, detected := range detectedContexts {
			if strings.EqualFold(ctxPolicy.Name, detected) {
				return ctxPolicy.Languages
			}
		}
	}

	// 3. Topic match
	if item.Topic != "" {
		if langs, ok := r.policy.Topic[item.Topic]; ok {
			return langs
		}
	}

	// 4. Default
	if len(r.policy.Default) > 0 {
		return r.policy.Default
	}

	return []string{"en"}
}

func (r *LanguageRouter) detectContexts(ctx context.Context, item *db.EnrichmentQueueItem) []string {
	var contexts []string

	// Check channel metadata
	for _, cp := range r.policy.Context {
		if r.matchesKeywords(item.ChannelTitle, cp.Keywords) ||
			r.matchesKeywords(item.ChannelDescription, cp.Keywords) ||
			r.matchesKeywords(item.Summary, cp.Keywords) ||
			r.matchesKeywords(item.Topic, cp.Keywords) {
			contexts = append(contexts, cp.Name)

			continue
		}

		// Check history (last 10 messages)
		if item.ChannelID != "" {
			history, err := r.db.GetRecentMessagesForChannel(ctx, item.ChannelID, time.Now(), historyLimit)
			if err == nil {
				for _, msg := range history {
					if r.matchesKeywords(msg, cp.Keywords) {
						contexts = append(contexts, cp.Name)

						break
					}
				}
			}
		}
	}

	return contexts
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
