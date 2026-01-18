package bot

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	"github.com/lueurxax/telegram-digest-bot/internal/output/digest"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
)

// Message size and delay constants.
const (
	// MaxMessageSize is the maximum size for a single Telegram message part.
	MaxMessageSize = 4000
	// SleepBetweenParts is the delay between sending message parts to avoid rate limits.
	SleepBetweenParts = 500 * time.Millisecond
	// SleepAfterImage is the delay after sending an image before sending text.
	SleepAfterImage = 300 * time.Millisecond
	// SleepBetweenRichItems is the delay between sending rich digest items.
	SleepBetweenRichItems = 200 * time.Millisecond
	// SummaryTruncateLength is the max length for summary in log messages.
	SummaryTruncateLength = 50
)

// Log field constants.
const (
	logFieldMimeType = "mime_type"
)

// Callback data prefixes.
const (
	CallbackPrefixRate     = "rate:"
	CallbackPrefixDiscover = "discover:"
	CallbackPrefixAnnotate = "annotate:"
	CallbackSuffixUp       = ":up"
	CallbackSuffixDown     = ":down"
)

// Command names.
const (
	CmdStatus             = "status"
	CmdSettings           = "settings"
	CmdHistory            = "history"
	CmdAdd                = "add"
	CmdList               = "list"
	CmdRemove             = "remove"
	CmdPrompt             = "prompt"
	CmdAnnotate           = "annotate"
	CmdMinLength          = "min_length"
	CmdMinLengthAlt       = "minlength"
	CmdSkipForwards       = "skip_forwards"
	CmdSkipFwdAlt         = "skipforwards"
	CmdTarget             = "target"
	CmdWindow             = "window"
	CmdSchedule           = "schedule"
	CmdTopics             = "topics"
	CmdDedup              = "dedup"
	CmdRelevance          = "relevance"
	CmdImportance         = "importance"
	CmdLanguage           = "language"
	CmdTone               = "tone"
	CmdModel              = "model"
	CmdSmartModel         = "smart_model"
	CmdSmartModelAlt      = "smartmodel"
	CmdEditor             = "editor"
	CmdTiered             = "tiered"
	CmdVision             = "vision"
	CmdVisionAlt          = "visionrouting"
	CmdConsolidated       = "consolidated"
	CmdEditorDetail       = "editordetails"
	CmdErrors             = "errors"
	CmdRetry              = "retry"
	CmdChannel            = "channel"
	CmdScores             = "scores"
	CmdFactCheck          = "factcheck"
	CmdRatings            = "ratings"
	CmdCoverImage         = "cover_image"
	CmdCoverImageAlt      = "coverimage"
	CmdAICover            = "ai_cover"
	CmdAICoverAlt         = "aicover"
	CmdInlineImages       = "inline_images"
	CmdInlineImagesAlt    = "inlineimages"
	CmdOthersNarrative    = "others_narrative"
	CmdOthersNarrativeAlt = "othersnarrative"
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
	SettingDigestCoverImage            = "digest_cover_image"
	SettingDigestAICover               = "digest_ai_cover"
	SettingDigestInlineImages          = "digest_inline_images"
	SettingOthersAsNarrative           = "others_as_narrative"
)

// Log field names.
const (
	LogFieldUserID   = "user_id"
	LogFieldUsername = "username"
	LogFieldAction   = "action"
)

// Button labels.
const (
	ButtonUseful    = "👍 Useful"
	ButtonNotUseful = "👎 Not useful"
)

// HTML tag constants.
const (
	htmlItalicClose = "</i>"
)

// Error message formats.
const (
	ErrSendDigestPart   = "failed to send digest part %d to chat %d: %w"
	ErrSendCallbackResp = "failed to send callback response"
)

type Bot struct {
	cfg           *config.Config
	database      Repository
	digestBuilder DigestBuilder
	llmClient     llm.Client
	api           *tgbotapi.BotAPI
	logger        *zerolog.Logger
}

func New(cfg *config.Config, database Repository, digestBuilder DigestBuilder, llmClient llm.Client, logger *zerolog.Logger) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		return nil, fmt.Errorf("creating bot API: %w", err)
	}

	return &Bot{
		cfg:           cfg,
		database:      database,
		digestBuilder: digestBuilder,
		llmClient:     llmClient,
		api:           api,
		logger:        logger,
	}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("bot run context canceled: %w", ctx.Err())
		case update := <-updates:
			if update.CallbackQuery != nil {
				b.handleCallback(ctx, update.CallbackQuery)
				continue
			}

			if update.Message == nil {
				continue
			}

			if !b.isAdmin(ctx, update.Message.From.ID) {
				b.logger.Warn().Int64(LogFieldUserID, update.Message.From.ID).Str(LogFieldUsername, update.Message.From.UserName).Msg("Unauthorized access attempt")
				continue
			}

			b.handleMessage(ctx, update.Message)
		}
	}
}

func (b *Bot) isAdmin(ctx context.Context, userID int64) bool {
	admins := b.getAdmins(ctx)

	for _, id := range admins {
		if id == userID {
			return true
		}
	}

	return false
}

func (b *Bot) getAdmins(ctx context.Context) []int64 {
	admins := make([]int64, 0, len(b.cfg.AdminIDs))
	admins = append(admins, b.cfg.AdminIDs...)

	// Check database settings for additional admins
	var extraAdmins []int64

	if err := b.database.GetSetting(ctx, "admin_ids", &extraAdmins); err == nil {
		admins = append(admins, extraAdmins...)
	}

	return admins
}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	if !msg.IsCommand() {
		return
	}

	b.logger.Info().Str("command", msg.Command()).Int64(LogFieldUserID, msg.From.ID).Msg("Handling command")

	registry := b.newCommandRegistry()
	if !registry.route(ctx, b, msg) {
		b.reply(msg, "Unknown command")
	}
}

func (b *Bot) handleCallback(ctx context.Context, query *tgbotapi.CallbackQuery) {
	if !b.isAdmin(ctx, query.From.ID) {
		return
	}

	data := query.Data

	switch {
	case strings.HasPrefix(data, CallbackPrefixRate):
		b.handleRateCallback(ctx, query, data)
	case strings.HasPrefix(data, CallbackPrefixDiscover):
		b.handleDiscoverCallback(ctx, query)
	case strings.HasPrefix(data, CallbackPrefixAnnotate):
		b.handleAnnotateCallback(ctx, query, data)
	}
}

func (b *Bot) handleRateCallback(ctx context.Context, query *tgbotapi.CallbackQuery, data string) {
	parts := strings.Split(data, ":")
	if len(parts) != 3 {
		return
	}

	digestID := parts[1]
	rating := parseRatingValue(parts[2])

	if rating == 0 {
		return
	}

	if err := b.database.SaveRating(ctx, digestID, query.From.ID, rating, ""); err != nil {
		b.logger.Error().Err(err).Msg("failed to save rating")
	}

	callback := tgbotapi.NewCallback(query.ID, "Feedback recorded. Thanks!")
	if _, err := b.api.Request(callback); err != nil {
		b.logger.Error().Err(err).Msg(ErrSendCallbackResp)
	}
}

func parseRatingValue(val string) int16 {
	switch val {
	case "up":
		return 1
	case "down":
		return -1
	default:
		return 0
	}
}

func (b *Bot) SendNotification(ctx context.Context, text string) error {
	admins := b.getAdmins(ctx)

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
	firstMsgID := b.sendCoverImage(chatID, imageData)

	// Send text parts
	parts := SplitHTML(text, MaxMessageSize)

	for i, part := range parts {
		msgID, err := b.sendDigestPart(chatID, part, digestID, i, len(parts))
		if err != nil {
			return 0, err
		}

		if firstMsgID == 0 {
			firstMsgID = msgID
		}
	}

	return firstMsgID, nil
}

// sendCoverImage sends the cover image and returns the message ID (0 if not sent).
func (b *Bot) sendCoverImage(chatID int64, imageData []byte) int64 {
	if len(imageData) == 0 {
		return 0
	}

	mimeType := http.DetectContentType(imageData)
	fileName := getImageFileName(mimeType)

	if fileName == "" {
		b.logger.Debug().Str(logFieldMimeType, mimeType).Msg("skipping unsupported image format for cover")

		return 0
	}

	photoMsg := tgbotapi.NewPhoto(chatID, tgbotapi.FileBytes{
		Name:  fileName,
		Bytes: imageData,
	})

	sent, err := b.api.Send(photoMsg)
	if err != nil {
		b.logger.Warn().Err(err).Str(logFieldMimeType, mimeType).Msg("failed to send digest cover image, continuing with text only")

		return 0
	}

	time.Sleep(SleepAfterImage)

	return int64(sent.MessageID)
}

// sendDigestPart sends a single part of the digest text.
func (b *Bot) sendDigestPart(chatID int64, part, digestID string, index, total int) (int64, error) {
	msg := tgbotapi.NewMessage(chatID, part)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.DisableWebPagePreview = true

	// Add rating buttons to the last part
	if index == total-1 && digestID != "" {
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(ButtonUseful, CallbackPrefixRate+digestID+CallbackSuffixUp),
				tgbotapi.NewInlineKeyboardButtonData(ButtonNotUseful, CallbackPrefixRate+digestID+CallbackSuffixDown),
			),
		)
	}

	sent, err := b.api.Send(msg)
	if err != nil {
		return 0, fmt.Errorf(ErrSendDigestPart, index+1, chatID, err)
	}

	if total > 1 && index < total-1 {
		time.Sleep(SleepBetweenParts)
	}

	return int64(sent.MessageID), nil
}

// getImageFileName returns the appropriate filename for a given MIME type.
// Returns empty string for unsupported formats (GIF, animated images).
func getImageFileName(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return "cover.jpg"
	case "image/png":
		return "cover.png"
	case "image/webp":
		// WebP can be animated, but static WebP is fine for Telegram
		return "cover.webp"
	case "image/gif":
		// Skip GIFs as they often look bad as cover images
		return ""
	default:
		// Unknown or unsupported format
		return ""
	}
}

// SendRichDigest sends a digest with inline images per item.
func (b *Bot) SendRichDigest(ctx context.Context, chatID int64, content digest.RichDigestContent) (int64, error) {
	var firstMsgID int64

	// Send header as text
	headerMsg := tgbotapi.NewMessage(chatID, content.Header)
	headerMsg.ParseMode = tgbotapi.ModeHTML
	headerMsg.DisableWebPagePreview = true

	sent, err := b.api.Send(headerMsg)
	if err != nil {
		return 0, fmt.Errorf("failed to send digest header: %w", err)
	}

	firstMsgID = int64(sent.MessageID)

	// Send each item
	for _, item := range content.Items {
		if err := b.sendDigestItem(chatID, item); err != nil {
			truncatedSummary := item.Summary[:min(SummaryTruncateLength, len(item.Summary))]
			b.logger.Warn().Err(err).Str("summary", truncatedSummary).Msg("failed to send digest item")
		}

		time.Sleep(SleepBetweenRichItems)
	}

	// Send rating buttons
	if content.DigestID != "" {
		ratingMsg := tgbotapi.NewMessage(chatID, "━━━━━━━━━━━━━━━━━━━━━━")
		ratingMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(ButtonUseful, CallbackPrefixRate+content.DigestID+CallbackSuffixUp),
				tgbotapi.NewInlineKeyboardButtonData(ButtonNotUseful, CallbackPrefixRate+content.DigestID+CallbackSuffixDown),
			),
		)

		if _, err := b.api.Send(ratingMsg); err != nil {
			b.logger.Warn().Err(err).Msg("failed to send rating buttons")
		}
	}

	return firstMsgID, nil
}

// sendDigestItem sends a single digest item as photo with caption or text.
func (b *Bot) sendDigestItem(chatID int64, item digest.RichDigestItem) error {
	// Format the item caption/text
	caption := formatDigestItemCaption(item)

	// Check if we have valid image data
	if len(item.MediaData) > 0 {
		mimeType := http.DetectContentType(item.MediaData)
		fileName := getImageFileName(mimeType)

		if fileName != "" {
			// Send as photo with caption
			photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileBytes{
				Name:  fileName,
				Bytes: item.MediaData,
			})
			photo.Caption = caption
			photo.ParseMode = tgbotapi.ModeHTML

			_, err := b.api.Send(photo)
			if err == nil {
				return nil
			}

			// If photo send fails, fall through to text
			b.logger.Debug().Err(err).Msg("photo send failed, falling back to text")
		}
	}

	// Send as text message
	msg := tgbotapi.NewMessage(chatID, caption)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.DisableWebPagePreview = true

	_, err := b.api.Send(msg)
	if err != nil {
		return fmt.Errorf("failed to send digest item: %w", err)
	}

	return nil
}

// formatDigestItemCaption formats a digest item for display.
func formatDigestItemCaption(item digest.RichDigestItem) string {
	var sb strings.Builder

	// Add topic emoji if available
	if item.Topic != "" {
		sb.WriteString(getTopicEmoji(item.Topic))
		sb.WriteString(" ")
	}

	// Add importance indicator
	if item.Importance >= 0.8 {
		sb.WriteString("🔴 ")
	} else if item.Importance >= 0.6 {
		sb.WriteString("📌 ")
	}

	// Add summary
	sb.WriteString(item.Summary)
	sb.WriteString("\n")

	// Add source link
	if item.Channel != "" {
		sb.WriteString("   ↳ <i>via ")

		if item.ChannelID != 0 && item.MsgID != 0 {
			sb.WriteString(FormatLink(item.Channel, item.ChannelID, item.MsgID, "@"+item.Channel))
		} else {
			sb.WriteString("@")
			sb.WriteString(item.Channel)
		}

		sb.WriteString(htmlItalicClose)
	}

	return sb.String()
}

// getTopicEmoji returns emoji for a topic.
func getTopicEmoji(topic string) string {
	topicEmojis := map[string]string{
		"Technology":    "💻",
		"Finance":       "💰",
		"Politics":      "⚖️",
		"Sports":        "🏆",
		"Entertainment": "🎬",
		"Science":       "🔬",
		"Health":        "🏥",
		"Business":      "📊",
		"World News":    "🌍",
		"Local News":    "📍",
		"Culture":       "🎨",
		"Education":     "📚",
		"Humor":         "😂",
	}

	if emoji, ok := topicEmojis[topic]; ok {
		return emoji
	}

	return "•"
}

func (b *Bot) reply(msg *tgbotapi.Message, text string) {
	b.sendMessage(msg.Chat.ID, text)
}

func (b *Bot) sendMessage(chatID int64, text string) {
	parts := SplitHTML(text, MaxMessageSize)

	for _, part := range parts {
		reply := tgbotapi.NewMessage(chatID, part)
		reply.ParseMode = tgbotapi.ModeHTML

		if _, err := b.api.Send(reply); err != nil {
			b.logger.Error().Err(err).Msg("failed to send reply")
		}
	}
}
