package telegrambot

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/config"
	"github.com/lueurxax/telegram-digest-bot/internal/db"
	"github.com/lueurxax/telegram-digest-bot/internal/llm"
)

type Bot struct {
	cfg       *config.Config
	database  *db.DB
	llmClient llm.Client
	api       *tgbotapi.BotAPI
	logger    *zerolog.Logger
}

func New(cfg *config.Config, database *db.DB, llmClient llm.Client, logger *zerolog.Logger) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		return nil, err
	}

	return &Bot{
		cfg:       cfg,
		database:  database,
		llmClient: llmClient,
		api:       api,
		logger:    logger,
	}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case update := <-updates:
			if update.CallbackQuery != nil {
				b.handleCallback(update.CallbackQuery)
				continue
			}

			if update.Message == nil {
				continue
			}

			if !b.isAdmin(update.Message.From.ID) {
				b.logger.Warn().Int64("user_id", update.Message.From.ID).Str("username", update.Message.From.UserName).Msg("Unauthorized access attempt")
				continue
			}

			b.handleMessage(update.Message)
		}
	}
}

func (b *Bot) isAdmin(userID int64) bool {
	admins := b.getAdmins()
	for _, id := range admins {
		if id == userID {
			return true
		}
	}
	return false
}

func (b *Bot) getAdmins() []int64 {
	admins := make([]int64, len(b.cfg.AdminIDs))
	copy(admins, b.cfg.AdminIDs)

	// Check database settings for additional admins
	var extraAdmins []int64
	ctx := context.Background()
	if err := b.database.GetSetting(ctx, "admin_ids", &extraAdmins); err == nil {
		admins = append(admins, extraAdmins...)
	}
	return admins
}

func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	if !msg.IsCommand() {
		return
	}

	b.logger.Info().Str("command", msg.Command()).Int64("user_id", msg.From.ID).Msg("Handling command")

	switch msg.Command() {
	case "start", "help":
		b.handleHelp(msg)
	case "setup":
		b.handleSetup(msg)
	case "status":
		b.handleStatus(msg)
	case "preview":
		b.handlePreview(msg)
	case "channel":
		b.handleChannelNamespace(msg)
	case "filter":
		b.handleFilterNamespace(msg)
	case "config":
		b.handleConfigNamespace(msg)
	case "ai":
		b.handleAINamespace(msg)
	case "system":
		b.handleSystemNamespace(msg)
	case "settings":
		b.handleSettings(msg)
	case "history":
		b.handleHistory(msg)
	case "add":
		b.handleAddChannel(msg)
	case "list":
		b.handleListChannels(msg)
	case "remove":
		b.handleRemoveChannel(msg)
	case "feedback":
		b.handleFeedback(msg)
	case "ratings":
		b.handleRatings(msg)
	case "channelcontext":
		b.handleChannelContext(msg)
	case "filters":
		b.handleFilters(msg)
	case "min_length", "minlength":
		b.handleMinLength(msg)
	case "ads_keywords", "adskeywords":
		b.handleAdsKeywords(msg)
	case "skip_forwards", "skipforwards":
		b.handleToggleSetting(msg, "filters_skip_forwards")
	case "target":
		b.handleTarget(msg)
	case "window":
		b.handleWindow(msg)
	case "topics":
		b.handleTopics(msg)
	case "dedup":
		b.handleDedup(msg)
	case "relevance":
		b.handleThreshold(msg, "relevance_threshold")
	case "importance":
		b.handleThreshold(msg, "importance_threshold")
	case "language":
		b.handleLanguage(msg)
	case "tone":
		b.handleTone(msg)
	case "model":
		b.handleModel(msg)
	case "smart_model", "smartmodel":
		b.handleSmartModel(msg)
	case "editor":
		b.handleToggleSetting(msg, "editor_enabled")
	case "tiered":
		b.handleToggleSetting(msg, "tiered_importance_enabled")
	case "vision", "vision_routing", "visionrouting":
		b.handleToggleSetting(msg, "vision_routing_enabled")
	case "consolidated":
		b.handleToggleSetting(msg, "consolidated_clusters_enabled")
	case "editor_details", "editordetails":
		b.handleToggleSetting(msg, "editor_detailed_items")
	case "errors":
		b.handleErrors(msg)
	case "retry":
		b.handleRetry(msg)
	case "discover":
		b.handleDiscoverNamespace(msg)
	default:
		b.reply(msg, "Unknown command")
	}
}

func (b *Bot) handleCallback(query *tgbotapi.CallbackQuery) {
	if !b.isAdmin(query.From.ID) {
		return
	}

	data := query.Data
	if strings.HasPrefix(data, "rate:") {
		parts := strings.Split(data, ":")
		if len(parts) == 3 {
			digestID := parts[1]
			ratingVal := parts[2]
			var rating int16
			switch ratingVal {
			case "up":
				rating = 1
			case "down":
				rating = -1
			}

			if rating != 0 {
				ctx := context.Background()
				if err := b.database.SaveRating(ctx, digestID, query.From.ID, rating, ""); err != nil {
					b.logger.Error().Err(err).Msg("failed to save rating")
				}

				callback := tgbotapi.NewCallback(query.ID, "Feedback recorded. Thanks!")
				if _, err := b.api.Request(callback); err != nil {
					b.logger.Error().Err(err).Msg("failed to send callback response")
				}
			}
		}
	} else if strings.HasPrefix(data, "discover:") {
		b.handleDiscoverCallback(query)
	}
}

func (b *Bot) SendNotification(ctx context.Context, text string) error {
	admins := b.getAdmins()
	for _, adminID := range admins {
		msg := tgbotapi.NewMessage(adminID, text)
		msg.ParseMode = tgbotapi.ModeHTML
		if _, err := b.api.Send(msg); err != nil {
			b.logger.Error().Err(err).Int64("admin_id", adminID).Msg("failed to send notification to admin")
		}
	}
	return nil
}

func (b *Bot) SendDigest(ctx context.Context, chatID int64, text string, digestID string) (int64, error) {
	parts := SplitHTML(text, 4000)
	var firstMsgID int64

	for i, part := range parts {
		msg := tgbotapi.NewMessage(chatID, part)
		msg.ParseMode = tgbotapi.ModeHTML
		msg.DisableWebPagePreview = true

		// Add rating buttons to the last part of the digest
		if i == len(parts)-1 && digestID != "" {
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("👍 Useful", "rate:"+digestID+":up"),
					tgbotapi.NewInlineKeyboardButtonData("👎 Not useful", "rate:"+digestID+":down"),
				),
			)
		}

		sent, err := b.api.Send(msg)
		if err != nil {
			return 0, fmt.Errorf("failed to send digest part %d to chat %d: %w", i+1, chatID, err)
		}
		if i == 0 {
			firstMsgID = int64(sent.MessageID)
		}

		// Small delay between parts to avoid rate limits if many parts
		if len(parts) > 1 && i < len(parts)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	return firstMsgID, nil
}

func (b *Bot) SendDigestWithImage(ctx context.Context, chatID int64, text string, digestID string, imageData []byte) (int64, error) {
	var firstMsgID int64

	// Send image first if provided
	if len(imageData) > 0 {
		photoMsg := tgbotapi.NewPhoto(chatID, tgbotapi.FileBytes{
			Name:  "cover.jpg",
			Bytes: imageData,
		})
		sent, err := b.api.Send(photoMsg)
		if err != nil {
			b.logger.Warn().Err(err).Msg("failed to send digest cover image, continuing with text only")
		} else {
			firstMsgID = int64(sent.MessageID)
			time.Sleep(300 * time.Millisecond)
		}
	}

	// Send text parts
	parts := SplitHTML(text, 4000)
	for i, part := range parts {
		msg := tgbotapi.NewMessage(chatID, part)
		msg.ParseMode = tgbotapi.ModeHTML
		msg.DisableWebPagePreview = true

		// Add rating buttons to the last part of the digest
		if i == len(parts)-1 && digestID != "" {
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("👍 Useful", "rate:"+digestID+":up"),
					tgbotapi.NewInlineKeyboardButtonData("👎 Not useful", "rate:"+digestID+":down"),
				),
			)
		}

		sent, err := b.api.Send(msg)
		if err != nil {
			return 0, fmt.Errorf("failed to send digest part %d to chat %d: %w", i+1, chatID, err)
		}
		if firstMsgID == 0 {
			firstMsgID = int64(sent.MessageID)
		}

		if len(parts) > 1 && i < len(parts)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	return firstMsgID, nil
}

func (b *Bot) reply(msg *tgbotapi.Message, text string) {
	parts := SplitHTML(text, 4000)
	for _, part := range parts {
		reply := tgbotapi.NewMessage(msg.Chat.ID, part)
		reply.ParseMode = tgbotapi.ModeHTML
		if _, err := b.api.Send(reply); err != nil {
			b.logger.Error().Err(err).Msg("failed to send reply")
		}
	}
}
