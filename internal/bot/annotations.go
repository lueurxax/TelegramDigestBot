package bot

import (
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/htmlutils"
	"github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	DefaultAnnotateHours = 24
	DefaultAnnotateLimit = 50

	annotationTextLimit        = 800
	annotationSummaryLimit     = 600
	annotationCallbackNumParts = 3 // prefix:action:itemID
	annotateEnqueueUsage       = "Usage: <code>/annotate enqueue [hours] [limit]</code>"
	annotateLabelUsage         = "Usage: <code>/annotate label &lt;good|bad|irrelevant&gt; [comment]</code>"
	annotateNoAssigned         = "No assigned annotation item. Use <code>/annotate next</code> first."
	annotateNoSummary          = "(no summary)"
	annotateBlockquoteFmt      = "<blockquote>%s</blockquote>\n"
	annotateUnknown            = "unknown"
	annotationActionSkip       = "skip"

	buttonAnnotateGood       = "👍 Good"
	buttonAnnotateBad        = "👎 Bad"
	buttonAnnotateIrrelevant = "🚫 Irrelevant"
	buttonAnnotateSkip       = "⏭ Skip"
)

func (b *Bot) handleAnnotate(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 {
		b.reply(msg, annotateUsage())
		return
	}

	switch strings.ToLower(args[0]) {
	case "enqueue":
		b.handleAnnotateEnqueue(ctx, msg, args[1:])
	case "next":
		b.handleAnnotateNext(ctx, msg)
	case "label":
		b.handleAnnotateLabel(ctx, msg, args[1:])
	case "skip":
		b.handleAnnotateSkip(ctx, msg)
	case SubCmdStats:
		b.handleAnnotateStats(ctx, msg)
	default:
		b.reply(msg, annotateUsage())
	}
}

func (b *Bot) handleAnnotateEnqueue(ctx context.Context, msg *tgbotapi.Message, args []string) {
	hours := DefaultAnnotateHours
	limit := DefaultAnnotateLimit

	if len(args) > 0 {
		if v, err := strconv.Atoi(args[0]); err == nil && v > 0 {
			hours = v
		} else {
			b.reply(msg, annotateEnqueueUsage)
			return
		}
	}

	if len(args) > 1 {
		if v, err := strconv.Atoi(args[1]); err == nil && v > 0 {
			limit = v
		} else {
			b.reply(msg, annotateEnqueueUsage)
			return
		}
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour)

	count, err := b.database.EnqueueAnnotationItems(ctx, since, limit)
	if err != nil {
		b.reply(msg, fmt.Sprintf("Error enqueuing annotation items: %s", html.EscapeString(err.Error())))
		return
	}

	b.reply(msg, fmt.Sprintf("Enqueued <code>%d</code> items from the last %d hours.", count, hours))
}

func (b *Bot) handleAnnotateNext(ctx context.Context, msg *tgbotapi.Message) {
	b.sendNextAnnotation(ctx, msg.Chat.ID, msg.From.ID)
}

func (b *Bot) handleAnnotateLabel(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) == 0 {
		b.reply(msg, annotateLabelUsage)
		return
	}

	label, ok := normalizeAnnotationLabel(args[0])
	if !ok {
		b.reply(msg, annotateLabelUsage)
		return
	}

	comment := strings.TrimSpace(strings.Join(args[1:], " "))

	item, err := b.database.LabelAssignedAnnotation(ctx, msg.From.ID, label, comment)
	if err != nil {
		b.reply(msg, fmt.Sprintf("Error saving annotation: %s", html.EscapeString(err.Error())))
		return
	}

	if item == nil {
		b.reply(msg, annotateNoAssigned)
		return
	}

	b.reply(msg, fmt.Sprintf("Labeled item <code>%s</code> as <code>%s</code>.", item.ItemID, label))
}

func (b *Bot) handleAnnotateSkip(ctx context.Context, msg *tgbotapi.Message) {
	item, err := b.database.SkipAssignedAnnotation(ctx, msg.From.ID)
	if err != nil {
		b.reply(msg, fmt.Sprintf("Error skipping annotation item: %s", html.EscapeString(err.Error())))
		return
	}

	if item == nil {
		b.reply(msg, annotateNoAssigned)
		return
	}

	b.reply(msg, fmt.Sprintf("Skipped item <code>%s</code>.", item.ItemID))
}

func (b *Bot) handleAnnotateStats(ctx context.Context, msg *tgbotapi.Message) {
	stats, err := b.database.GetAnnotationStats(ctx)
	if err != nil {
		b.reply(msg, fmt.Sprintf("Error fetching annotation stats: %s", html.EscapeString(err.Error())))
		return
	}

	if len(stats) == 0 {
		b.reply(msg, "No annotation queue entries yet.")
		return
	}

	var (
		total int
		sb    strings.Builder
	)

	sb.WriteString("<b>Annotation Queue</b>\n\n")

	for _, status := range []string{
		db.AnnotationStatusPending,
		db.AnnotationStatusAssigned,
		db.AnnotationStatusLabeled,
		db.AnnotationStatusSkipped,
	} {
		count := stats[status]
		total += count
		sb.WriteString(fmt.Sprintf(statsItemFormat, status, count))
	}

	sb.WriteString(fmt.Sprintf("\nTotal: <code>%d</code>\n", total))
	b.reply(msg, sb.String())
}

func annotateUsage() string {
	return "Usage:\n" +
		"<code>/annotate enqueue [hours] [limit]</code>\n" +
		"<code>/annotate next</code>\n" +
		"<code>/annotate label &lt;good|bad|irrelevant&gt; [comment]</code>\n" +
		"<code>/annotate skip</code>\n" +
		"<code>/annotate stats</code>\n" +
		"Tap buttons on annotation cards to label quickly."
}

func normalizeAnnotationLabel(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case RatingGood:
		return RatingGood, true
	case RatingBad:
		return RatingBad, true
	case RatingIrrelevant:
		return RatingIrrelevant, true
	default:
		return "", false
	}
}

func formatAnnotationItem(item *db.AnnotationItem) string {
	var sb strings.Builder

	sb.WriteString("<b>Annotation Item</b>\n\n")

	name := annotationChannelName(item)
	link := FormatLink(item.ChannelUsername, item.ChannelPeerID, item.MessageID, "Open message")

	sb.WriteString(fmt.Sprintf("Channel: <b>%s</b>\n", html.EscapeString(name)))
	sb.WriteString(fmt.Sprintf("Message: %s\n", link))
	sb.WriteString(fmt.Sprintf("Item: <code>%s</code>\n", item.ItemID))
	sb.WriteString(fmt.Sprintf("Time: <code>%s</code>\n", item.TGDate.Format(DateTimeFormat)))
	sb.WriteString(fmt.Sprintf("Status: <code>%s</code>\n", html.EscapeString(item.Status)))
	sb.WriteString(fmt.Sprintf("Scores: rel <code>%.2f</code> | imp <code>%.2f</code>\n", item.RelevanceScore, item.ImportanceScore))

	if item.Topic != "" {
		sb.WriteString(fmt.Sprintf("Topic: <code>%s</code>\n", html.EscapeString(item.Topic)))
	}

	summary := strings.TrimSpace(item.Summary)
	if summary == "" {
		summary = annotateNoSummary
	} else {
		summary = htmlutils.StripHTMLTags(summary)
	}

	summary = truncateAnnotationText(summary, annotationSummaryLimit)
	summary = html.EscapeString(summary)

	sb.WriteString("\nSummary:\n")
	sb.WriteString(fmt.Sprintf(annotateBlockquoteFmt, summary))

	text := strings.TrimSpace(item.Text)
	if text != "" {
		text = truncateAnnotationText(text, annotationTextLimit)
		text = html.EscapeString(text)

		sb.WriteString("Text:\n")
		sb.WriteString(fmt.Sprintf(annotateBlockquoteFmt, text))
	}

	sb.WriteString("\nLabel with buttons below or <code>/annotate label good|bad|irrelevant [comment]</code> or <code>/annotate skip</code>.")

	return sb.String()
}

func annotationChannelName(item *db.AnnotationItem) string {
	if item.ChannelUsername != "" {
		return "@" + item.ChannelUsername
	}

	if item.ChannelTitle != "" {
		return item.ChannelTitle
	}

	return annotateUnknown
}

func truncateAnnotationText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}

	return string(runes[:limit]) + "..."
}

func (b *Bot) handleAnnotateCallback(ctx context.Context, query *tgbotapi.CallbackQuery, data string) {
	parts := strings.SplitN(data, ":", annotationCallbackNumParts)
	if len(parts) != annotationCallbackNumParts {
		return
	}

	action := parts[1]
	itemID := parts[2]

	var (
		item *db.AnnotationItem
		err  error
	)

	switch action {
	case RatingGood, RatingBad, RatingIrrelevant:
		item, err = b.database.LabelAnnotationByItem(ctx, query.From.ID, itemID, action, "")
	case annotationActionSkip:
		item, err = b.database.SkipAnnotationByItem(ctx, query.From.ID, itemID)
	default:
		return
	}

	if err != nil {
		b.logger.Error().Err(err).Msg("failed to update annotation from callback")
		b.answerCallback(query, "Error saving annotation.")

		return
	}

	if item == nil {
		b.answerCallback(query, "No assigned annotation item.")
		return
	}

	b.answerCallback(query, "Saved. Sending next...")

	if query.Message == nil {
		return
	}

	b.clearAnnotationButtons(query.Message.Chat.ID, query.Message.MessageID)
	b.sendNextAnnotation(ctx, query.Message.Chat.ID, query.From.ID)
}

func (b *Bot) sendNextAnnotation(ctx context.Context, chatID int64, userID int64) {
	item, err := b.database.AssignNextAnnotation(ctx, userID)
	if err != nil {
		b.sendMessage(chatID, fmt.Sprintf("Error fetching annotation item: %s", html.EscapeString(err.Error())))
		return
	}

	if item == nil {
		b.sendMessage(chatID, "No pending annotation items. Use <code>/annotate enqueue [hours] [limit]</code>.")
		return
	}

	b.sendAnnotationItem(chatID, item)
}

func (b *Bot) sendAnnotationItem(chatID int64, item *db.AnnotationItem) {
	text := formatAnnotationItem(item)
	parts := SplitHTML(text, MaxMessageSize)
	keyboard := annotationKeyboard(item.ItemID)

	for i, part := range parts {
		msg := tgbotapi.NewMessage(chatID, part)
		msg.ParseMode = tgbotapi.ModeHTML
		msg.DisableWebPagePreview = true

		if i == len(parts)-1 {
			msg.ReplyMarkup = keyboard
		}

		if _, err := b.api.Send(msg); err != nil {
			if strings.Contains(err.Error(), "can't parse entities") {
				fallback := htmlutils.SanitizeHTML(html.UnescapeString(part))
				fallbackMsg := tgbotapi.NewMessage(chatID, fallback)
				fallbackMsg.ParseMode = tgbotapi.ModeHTML
				fallbackMsg.DisableWebPagePreview = true

				if i == len(parts)-1 {
					fallbackMsg.ReplyMarkup = keyboard
				}

				if _, fallbackErr := b.api.Send(fallbackMsg); fallbackErr != nil {
					b.logger.Error().Err(fallbackErr).Msg("failed to send annotation item fallback")
				}

				continue
			}

			b.logger.Error().Err(err).Msg("failed to send annotation item")
		}
	}
}

func annotationKeyboard(itemID string) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(buttonAnnotateGood, annotationCallbackData(RatingGood, itemID)),
			tgbotapi.NewInlineKeyboardButtonData(buttonAnnotateBad, annotationCallbackData(RatingBad, itemID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(buttonAnnotateIrrelevant, annotationCallbackData(RatingIrrelevant, itemID)),
			tgbotapi.NewInlineKeyboardButtonData(buttonAnnotateSkip, annotationCallbackData(annotationActionSkip, itemID)),
		),
	)
}

func annotationCallbackData(action, itemID string) string {
	return CallbackPrefixAnnotate + action + ":" + itemID
}

func (b *Bot) answerCallback(query *tgbotapi.CallbackQuery, text string) {
	callback := tgbotapi.NewCallback(query.ID, text)
	if _, err := b.api.Request(callback); err != nil {
		b.logger.Error().Err(err).Msg(ErrSendCallbackResp)
	}
}

func (b *Bot) clearAnnotationButtons(chatID int64, messageID int) {
	emptyKeyboard := tgbotapi.InlineKeyboardMarkup{
		InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{},
	}

	edit := tgbotapi.NewEditMessageReplyMarkup(chatID, messageID, emptyKeyboard)

	if _, err := b.api.Request(edit); err != nil {
		b.logger.Debug().Err(err).Msg("failed to clear annotation buttons")
	}
}
