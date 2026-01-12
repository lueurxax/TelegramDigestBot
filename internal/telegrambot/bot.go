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

// Message size and delay constants.
const (
	// MaxMessageSize is the maximum size for a single Telegram message part.
	MaxMessageSize = 4000
	// SleepBetweenParts is the delay between sending message parts to avoid rate limits.
	SleepBetweenParts = 500 * time.Millisecond
	// SleepAfterImage is the delay after sending an image before sending text.
	SleepAfterImage = 300 * time.Millisecond
)

// Callback data prefixes.
const (
	CallbackPrefixRate     = "rate:"
	CallbackPrefixDiscover = "discover:"
	CallbackSuffixUp       = ":up"
	CallbackSuffixDown     = ":down"
)

// Command names.
const (
	CmdStatus        = "status"
	CmdSettings      = "settings"
	CmdHistory       = "history"
	CmdAdd           = "add"
	CmdList          = "list"
	CmdRemove        = "remove"
	CmdPrompt        = "prompt"
	CmdMinLength     = "min_length"
	CmdMinLengthAlt  = "minlength"
	CmdSkipForwards  = "skip_forwards"
	CmdSkipFwdAlt    = "skipforwards"
	CmdTarget        = "target"
	CmdWindow        = "window"
	CmdTopics        = "topics"
	CmdDedup         = "dedup"
	CmdRelevance     = "relevance"
	CmdImportance    = "importance"
	CmdLanguage      = "language"
	CmdTone          = "tone"
	CmdModel         = "model"
	CmdSmartModel    = "smart_model"
	CmdSmartModelAlt = "smartmodel"
	CmdEditor        = "editor"
	CmdTiered        = "tiered"
	CmdVision        = "vision"
	CmdVisionAlt     = "visionrouting"
	CmdConsolidated  = "consolidated"
	CmdEditorDetail  = "editordetails"
	CmdErrors        = "errors"
	CmdRetry         = "retry"
	CmdChannel       = "channel"
	CmdScores        = "scores"
	CmdRatings       = "ratings"
)

// Setting keys.
const (
	SettingFiltersSkipForwards         = "filters_skip_forwards"
	SettingRelevanceThreshold          = "relevance_threshold"
	SettingImportanceThreshold         = "importance_threshold"
	SettingEditorEnabled               = "editor_enabled"
	SettingTieredImportanceEnabled     = "tiered_importance_enabled"
	SettingVisionRoutingEnabled        = "vision_routing_enabled"
	SettingConsolidatedClustersEnabled = "consolidated_clusters_enabled"
	SettingEditorDetailedItems         = "editor_detailed_items"
)

// Log field names.
const (
	LogFieldUserID   = "user_id"
	LogFieldUsername = "username"
)

// Button labels.
const (
	ButtonUseful    = "👍 Useful"
	ButtonNotUseful = "👎 Not useful"
)

// Error message formats.
const (
	ErrSendDigestPart   = "failed to send digest part %d to chat %d: %w"
	ErrSendCallbackResp = "failed to send callback response"
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
				b.logger.Warn().Int64(LogFieldUserID, update.Message.From.ID).Str(LogFieldUsername, update.Message.From.UserName).Msg("Unauthorized access attempt")
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
	admins := make([]int64, 0, len(b.cfg.AdminIDs))
	admins = append(admins, b.cfg.AdminIDs...)

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

	b.logger.Info().Str("command", msg.Command()).Int64(LogFieldUserID, msg.From.ID).Msg("Handling command")

	switch msg.Command() {
	case "start", "help":
		b.handleHelp(msg)
	case "setup":
		b.handleSetup(msg)
	case CmdStatus:
		b.handleStatus(msg)
	case "preview":
		b.handlePreview(msg)
	case CmdChannel:
		b.handleChannelNamespace(msg)
	case "filter":
		b.handleFilterNamespace(msg)
	case "config":
		b.handleConfigNamespace(msg)
	case "ai":
		b.handleAINamespace(msg)
	case "system":
		b.handleSystemNamespace(msg)
	case CmdSettings:
		b.handleSettings(msg)
	case CmdHistory:
		b.handleHistory(msg)
	case CmdAdd:
		b.handleAddChannel(msg)
	case CmdList:
		b.handleListChannels(msg)
	case CmdRemove:
		b.handleRemoveChannel(msg)
	case "feedback":
		b.handleFeedback(msg)
	case CmdRatings:
		b.handleRatings(msg)
	case CmdScores:
		b.handleScores(msg)
	case CmdPrompt:
		b.handlePrompt(msg)
	case "channelcontext":
		b.handleChannelContext(msg)
	case "filters":
		b.handleFilters(msg)
	case CmdMinLength, CmdMinLengthAlt:
		b.handleMinLength(msg)
	case "ads_keywords", "adskeywords":
		b.handleAdsKeywords(msg)
	case CmdSkipForwards, CmdSkipFwdAlt:
		b.handleToggleSetting(msg, SettingFiltersSkipForwards)
	case CmdTarget:
		b.handleTarget(msg)
	case CmdWindow:
		b.handleWindow(msg)
	case CmdTopics:
		b.handleTopics(msg)
	case CmdDedup:
		b.handleDedup(msg)
	case CmdRelevance:
		b.handleThreshold(msg, SettingRelevanceThreshold)
	case CmdImportance:
		b.handleThreshold(msg, SettingImportanceThreshold)
	case CmdLanguage:
		b.handleLanguage(msg)
	case CmdTone:
		b.handleTone(msg)
	case CmdModel:
		b.handleModel(msg)
	case CmdSmartModel, CmdSmartModelAlt:
		b.handleSmartModel(msg)
	case CmdEditor:
		b.handleToggleSetting(msg, SettingEditorEnabled)
	case CmdTiered:
		b.handleToggleSetting(msg, SettingTieredImportanceEnabled)
	case CmdVision, "vision_routing", CmdVisionAlt:
		b.handleToggleSetting(msg, SettingVisionRoutingEnabled)
	case CmdConsolidated:
		b.handleToggleSetting(msg, SettingConsolidatedClustersEnabled)
	case "editor_details", CmdEditorDetail:
		b.handleToggleSetting(msg, SettingEditorDetailedItems)
	case CmdErrors:
		b.handleErrors(msg)
	case CmdRetry:
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

	if strings.HasPrefix(data, CallbackPrefixRate) {
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
					b.logger.Error().Err(err).Msg(ErrSendCallbackResp)
				}
			}
		}
	} else if strings.HasPrefix(data, CallbackPrefixDiscover) {
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
	parts := SplitHTML(text, MaxMessageSize)

	var firstMsgID int64

	for i, part := range parts {
		msg := tgbotapi.NewMessage(chatID, part)
		msg.ParseMode = tgbotapi.ModeHTML
		msg.DisableWebPagePreview = true

		// Add rating buttons to the last part of the digest
		if i == len(parts)-1 && digestID != "" {
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(ButtonUseful, CallbackPrefixRate+digestID+CallbackSuffixUp),
					tgbotapi.NewInlineKeyboardButtonData(ButtonNotUseful, CallbackPrefixRate+digestID+CallbackSuffixDown),
				),
			)
		}

		sent, err := b.api.Send(msg)
		if err != nil {
			return 0, fmt.Errorf(ErrSendDigestPart, i+1, chatID, err)
		}

		if i == 0 {
			firstMsgID = int64(sent.MessageID)
		}

		// Small delay between parts to avoid rate limits if many parts
		if len(parts) > 1 && i < len(parts)-1 {
			time.Sleep(SleepBetweenParts)
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

			time.Sleep(SleepAfterImage)
		}
	}

	// Send text parts
	parts := SplitHTML(text, MaxMessageSize)

	for i, part := range parts {
		msg := tgbotapi.NewMessage(chatID, part)
		msg.ParseMode = tgbotapi.ModeHTML
		msg.DisableWebPagePreview = true

		// Add rating buttons to the last part of the digest
		if i == len(parts)-1 && digestID != "" {
			msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(ButtonUseful, CallbackPrefixRate+digestID+CallbackSuffixUp),
					tgbotapi.NewInlineKeyboardButtonData(ButtonNotUseful, CallbackPrefixRate+digestID+CallbackSuffixDown),
				),
			)
		}

		sent, err := b.api.Send(msg)
		if err != nil {
			return 0, fmt.Errorf(ErrSendDigestPart, i+1, chatID, err)
		}

		if firstMsgID == 0 {
			firstMsgID = int64(sent.MessageID)
		}

		if len(parts) > 1 && i < len(parts)-1 {
			time.Sleep(SleepBetweenParts)
		}
	}

	return firstMsgID, nil
}

func (b *Bot) reply(msg *tgbotapi.Message, text string) {
	parts := SplitHTML(text, MaxMessageSize)

	for _, part := range parts {
		reply := tgbotapi.NewMessage(msg.Chat.ID, part)

		reply.ParseMode = tgbotapi.ModeHTML
		if _, err := b.api.Send(reply); err != nil {
			b.logger.Error().Err(err).Msg("failed to send reply")
		}
	}
}
