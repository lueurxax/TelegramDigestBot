package telegrambot

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/lueurxax/telegram-digest-bot/internal/db"
	"github.com/lueurxax/telegram-digest-bot/internal/digest"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func (b *Bot) handleThreshold(msg *tgbotapi.Message, key string) {
	args := msg.CommandArguments()

	if args == "" {
		// Derive command name from key (e.g., "relevance_threshold" -> "relevance")
		cmdName := strings.TrimSuffix(key, "_threshold")
		b.reply(msg, fmt.Sprintf("Usage: <code>/%s &lt;0.0-1.0&gt;</code>", html.EscapeString(cmdName)))

		return
	}

	val, err := strconv.ParseFloat(args, 32)

	if err != nil || val < 0 || val > 1 {
		b.reply(msg, "❌ Invalid value. Please provide a number between 0.0 and 1.0.")

		return
	}

	ctx := context.Background()

	var current float32

	_ = b.database.GetSetting(ctx, key, &current)

	if err := b.database.SaveSettingWithHistory(ctx, key, float32(val), msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving %s: %s", html.EscapeString(key), html.EscapeString(err.Error())))

		return
	}

	label := cases.Title(language.English).String(strings.ReplaceAll(key, "_", " "))
	b.reply(msg, fmt.Sprintf("✅ <b>%s</b> updated.\nOld value: <code>%v</code>\nNew value: <code>%v</code>", html.EscapeString(label), current, val))
}

func (b *Bot) handleStatus(msg *tgbotapi.Message) {
	ctx := context.Background()
	backlog, _ := b.database.GetBacklogCount(ctx)
	activeChannels, _ := b.database.CountActiveChannels(ctx)
	recentChannels, _ := b.database.CountRecentlyActiveChannels(ctx)
	readyItems, _ := b.database.CountReadyItems(ctx)
	lastDigest, _ := b.database.GetLastPostedDigest(ctx)

	var sb strings.Builder

	sb.WriteString("📊 <b>System Status</b>\n\n")
	sb.WriteString(fmt.Sprintf("• <b>Active Channels:</b> <code>%d</code>\n", activeChannels))
	sb.WriteString(fmt.Sprintf("• <b>Channels with messages (24h):</b> <code>%d</code>\n", recentChannels))
	sb.WriteString(fmt.Sprintf("• <b>Message Backlog:</b> <code>%d</code>\n", backlog))
	sb.WriteString(fmt.Sprintf("• <b>Items ready for digest:</b> <code>%d</code>\n", readyItems))

	if lastDigest != nil {
		sb.WriteString(fmt.Sprintf("• <b>Last Digest:</b> <code>%s</code>\n", lastDigest.PostedAt.Format("2006-01-02 15:04:05")))
		sb.WriteString(fmt.Sprintf("  <i>Window: %s - %s</i>\n", lastDigest.Start.Format("15:04"), lastDigest.End.Format("15:04")))
	} else {
		sb.WriteString("• <b>Last Digest:</b> <code>None</code>\n")
	}

	b.reply(msg, sb.String())
}

func (b *Bot) handleChannelNamespace(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.reply(msg, "Usage: <code>/channel &lt;add|remove|list|context|metadata|weight&gt;</code>")

		return
	}

	subcommand := args[0]
	// Rewrite msg to look like the subcommand was the command for easier reuse of existing handlers
	// This is a bit hacky but avoids duplicating logic
	newMsg := *msg
	newMsg.Text = "/" + subcommand

	if len(args) > 1 {
		newMsg.Text += " " + strings.Join(args[1:], " ")
	}

	// Update entities to match new text - the command entity length must match the new command
	newEntities := make([]tgbotapi.MessageEntity, len(msg.Entities))
	copy(newEntities, msg.Entities)

	for i := range newEntities {
		if newEntities[i].Type == "bot_command" && newEntities[i].Offset == 0 {
			newEntities[i].Length = len(subcommand) + 1 // +1 for the leading /
		}
	}

	newMsg.Entities = newEntities

	switch subcommand {
	case "add":
		b.handleAddChannel(&newMsg)
	case "remove":
		b.handleRemoveChannel(&newMsg)
	case "list":
		b.handleListChannels(&newMsg)
	case "context":
		b.handleChannelContext(&newMsg)
	case "metadata":
		b.handleChannelMetadata(&newMsg)
	case "stats":
		b.handleChannelStats(&newMsg)
	case "weight":
		b.handleChannelWeight(&newMsg)
	default:
		b.reply(msg, fmt.Sprintf("❓ Unknown channel subcommand: <code>%s</code>", html.EscapeString(subcommand)))
	}
}

func (b *Bot) handleFilterNamespace(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.handleFilters(msg)

		return
	}

	subcommand := args[0]
	newMsg := *msg
	newMsg.Text = "/" + subcommand

	if len(args) > 1 {
		newMsg.Text += " " + strings.Join(args[1:], " ")
	}

	// Update entities to match new text - the command entity length must match the new command
	newEntities := make([]tgbotapi.MessageEntity, len(msg.Entities))
	copy(newEntities, msg.Entities)

	for i := range newEntities {
		if newEntities[i].Type == "bot_command" && newEntities[i].Offset == 0 {
			newEntities[i].Length = len(subcommand) + 1 // +1 for the leading /
		}
	}

	newMsg.Entities = newEntities

	switch subcommand {
	case "add", "remove", "ads", "mode":
		b.handleFilters(msg) // handleFilters already handles these subcommands
	case "keywords":
		b.handleAdsKeywords(&newMsg)
	case "min_length", "minlength":
		b.handleMinLength(&newMsg)
	case "skip_forwards", "skipforwards":
		b.handleToggleSetting(&newMsg, "filters_skip_forwards")
	default:
		b.reply(msg, fmt.Sprintf("❓ Unknown filter subcommand: <code>%s</code>", html.EscapeString(subcommand)))
	}
}

func (b *Bot) handleConfigNamespace(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.reply(msg, "Usage: <code>/config &lt;links|max_links|link_cache|target|window|language|tone|relevance|importance|reset&gt;</code>")

		return
	}

	subcommand := args[0]
	newMsg := *msg
	newMsg.Text = "/" + subcommand

	if len(args) > 1 {
		newMsg.Text += " " + strings.Join(args[1:], " ")
	}

	// Update entities to match new text - the command entity length must match the new command
	newEntities := make([]tgbotapi.MessageEntity, len(msg.Entities))
	copy(newEntities, msg.Entities)

	for i := range newEntities {
		if newEntities[i].Type == "bot_command" && newEntities[i].Offset == 0 {
			newEntities[i].Length = len(subcommand) + 1 // +1 for the leading /
		}
	}

	newMsg.Entities = newEntities

	switch subcommand {
	case "links":
		b.handleToggleSetting(&newMsg, "link_enrichment_enabled")
	case "max_links", "maxlinks":
		b.handleMaxLinks(&newMsg)
	case "link_cache", "linkcache":
		b.handleLinkCache(&newMsg)
	case "target":
		b.handleTarget(&newMsg)
	case "window":
		b.handleWindow(&newMsg)
	case "language":
		b.handleLanguage(&newMsg)
	case "tone":
		b.handleTone(&newMsg)
	case "relevance":
		b.handleThreshold(&newMsg, "relevance_threshold")
	case "importance":
		b.handleThreshold(&newMsg, "importance_threshold")
	case "reset":
		b.handleSettings(&newMsg)
	default:
		b.reply(msg, fmt.Sprintf("❓ Unknown config subcommand: <code>%s</code>", html.EscapeString(subcommand)))
	}
}

func (b *Bot) handleAINamespace(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.reply(msg, "Usage: <code>/ai &lt;model|smart_model|tone|editor|tiered|vision|consolidated|details|topics|dedup&gt;</code>")

		return
	}

	subcommand := args[0]
	newMsg := *msg
	newMsg.Text = "/" + subcommand

	if len(args) > 1 {
		newMsg.Text += " " + strings.Join(args[1:], " ")
	}

	// Update entities to match new text - the command entity length must match the new command
	newEntities := make([]tgbotapi.MessageEntity, len(msg.Entities))
	copy(newEntities, msg.Entities)

	for i := range newEntities {
		if newEntities[i].Type == "bot_command" && newEntities[i].Offset == 0 {
			newEntities[i].Length = len(subcommand) + 1 // +1 for the leading /
		}
	}

	newMsg.Entities = newEntities

	switch subcommand {
	case "model":
		b.handleModel(&newMsg)
	case "smart_model", "smartmodel":
		b.handleSmartModel(&newMsg)
	case "prompt":
		b.handlePrompt(&newMsg)
	case "tone":
		b.handleTone(&newMsg)
	case "editor":
		b.handleToggleSetting(&newMsg, "editor_enabled")
	case "tiered":
		b.handleToggleSetting(&newMsg, "tiered_importance_enabled")
	case "vision", "visionrouting":
		b.handleToggleSetting(&newMsg, "vision_routing_enabled")
	case "consolidated":
		b.handleToggleSetting(&newMsg, "consolidated_clusters_enabled")
	case "normalize":
		b.handleToggleSetting(&newMsg, "normalize_scores")
	case "details", "editordetails":
		b.handleToggleSetting(&newMsg, "editor_detailed_items")
	case "topics":
		b.handleTopics(&newMsg)
	case "dedup":
		b.handleDedup(&newMsg)
	default:
		b.reply(msg, fmt.Sprintf("❓ Unknown AI subcommand: <code>%s</code>", html.EscapeString(subcommand)))
	}
}

func (b *Bot) handleSystemNamespace(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.reply(msg, "Usage: <code>/system &lt;status|settings|history|errors|retry&gt;</code>")

		return
	}

	subcommand := args[0]
	newMsg := *msg
	newMsg.Text = "/" + subcommand

	if len(args) > 1 {
		newMsg.Text += " " + strings.Join(args[1:], " ")
	}

	// Update entities to match new text - the command entity length must match the new command
	newEntities := make([]tgbotapi.MessageEntity, len(msg.Entities))
	copy(newEntities, msg.Entities)

	for i := range newEntities {
		if newEntities[i].Type == "bot_command" && newEntities[i].Offset == 0 {
			newEntities[i].Length = len(subcommand) + 1 // +1 for the leading /
		}
	}

	newMsg.Entities = newEntities

	switch subcommand {
	case "status":
		b.handleStatus(&newMsg)
	case "settings":
		b.handleSettings(&newMsg)
	case "history":
		b.handleHistory(&newMsg)
	case "errors":
		b.handleErrors(&newMsg)
	case "retry":
		b.handleRetry(&newMsg)
	default:
		b.reply(msg, fmt.Sprintf("❓ Unknown system subcommand: <code>%s</code>", html.EscapeString(subcommand)))
	}
}

func (b *Bot) handleTarget(msg *tgbotapi.Message) {
	args := msg.CommandArguments()

	if args == "" {
		b.reply(msg, "Usage: <code>/target &lt;channel_id or @username&gt;</code>")

		return
	}

	var chatID int64

	var chat tgbotapi.Chat

	var err error

	if strings.HasPrefix(args, "@") {
		chat, err = b.api.GetChat(tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{SuperGroupUsername: args}})
		if err != nil {
			// Try without @ if it's a username
			username := strings.TrimPrefix(args, "@")

			chat, err = b.api.GetChat(tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{SuperGroupUsername: username}})
			if err != nil {
				b.reply(msg, fmt.Sprintf("❌ Could not find chat %s: %s. Make sure the bot is an administrator in the channel.", html.EscapeString(args), html.EscapeString(err.Error())))

				return
			}
		}

		chatID = chat.ID
	} else {
		chatID, err = strconv.ParseInt(args, 10, 64)
		if err != nil {
			b.reply(msg, "❌ Invalid channel ID. It should be a number (don't forget the <code>-100</code> prefix for channels) or a <code>@username</code>.")

			return
		}

		// Verify bot has access to the chat
		chat, err = b.api.GetChat(tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: chatID}})
		if err != nil {
			// If it's a positive number, try adding -100 prefix (common for channels)
			if chatID > 0 {
				altID, _ := strconv.ParseInt("-100"+strconv.FormatInt(chatID, 10), 10, 64)

				var errAlt error

				chat, errAlt = b.api.GetChat(tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: altID}})
				if errAlt == nil {
					chatID = altID
				} else {
					b.reply(msg, fmt.Sprintf("❌ Could not find chat %d (nor %d): %s. Make sure the bot is added to the chat.", chatID, altID, html.EscapeString(errAlt.Error())))

					return
				}
			} else {
				b.reply(msg, fmt.Sprintf("❌ Could not find chat %d: %s. Make sure the bot is added to the chat.", chatID, html.EscapeString(err.Error())))

				return
			}
		}
	}

	// Try to send a test message to verify permissions
	testMsg := tgbotapi.NewMessage(chatID, "✅ This channel has been set as the target for digest posts.")
	if _, err := b.api.Send(testMsg); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Found chat <b>%s</b> but could not send a message to it: %s. Make sure the bot is an administrator with permission to post messages.", html.EscapeString(chat.Title), html.EscapeString(err.Error())))

		return
	}

	ctx := context.Background()

	if err := b.database.SaveSettingWithHistory(ctx, "target_chat_id", chatID, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving target chat ID: %s", html.EscapeString(err.Error())))

		return
	}

	// Clear any previous digest errors so the scheduler can retry with the new target
	if err := b.database.ClearDigestErrors(ctx); err != nil {
		b.logger.Warn().Err(err).Msg("failed to clear digest errors after target update")
	}

	b.reply(msg, fmt.Sprintf("✅ Target chat updated to <code>%d</code> (<b>%s</b>). A confirmation message has been sent to that channel.", chatID, html.EscapeString(chat.Title)))
}

func (b *Bot) handleWindow(msg *tgbotapi.Message) {
	args := msg.CommandArguments()

	if args == "" {
		b.reply(msg, "Usage: <code>/window &lt;duration&gt;</code> (e.g. <code>60m</code>, <code>6h</code>, <code>24h</code>)")

		return
	}

	_, err := time.ParseDuration(args)
	if err != nil {
		b.reply(msg, "❌ Invalid duration format. Use something like <code>60m</code>, <code>6h</code>, <code>24h</code>.")

		return
	}

	ctx := context.Background()

	if err := b.database.SaveSettingWithHistory(ctx, "digest_window", args, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving digest window: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Digest window updated to <code>%s</code>.", html.EscapeString(args)))
}

func (b *Bot) handleLanguage(msg *tgbotapi.Message) {
	args := msg.CommandArguments()

	if args == "" {
		b.reply(msg, "Usage: <code>/language &lt;lang_code&gt;</code> (e.g. <code>en</code>, <code>ru</code>, <code>de</code>)")

		return
	}

	ctx := context.Background()

	if err := b.database.SaveSettingWithHistory(ctx, "digest_language", args, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving digest language: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Digest language updated to <code>%s</code>.", html.EscapeString(args)))
}

func (b *Bot) handleMinLength(msg *tgbotapi.Message) {
	args := msg.CommandArguments()

	if args == "" {
		b.reply(msg, "Usage: <code>/minlength &lt;number&gt;</code>")

		return
	}

	val, err := strconv.Atoi(args)

	if err != nil || val < 0 {
		b.reply(msg, "❌ Invalid value. Please provide a positive number.")

		return
	}

	ctx := context.Background()

	if err := b.database.SaveSettingWithHistory(ctx, "filters_min_length", val, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving min length: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Minimum message length updated to <code>%d</code>.", val))
}

func (b *Bot) handleMaxLinks(msg *tgbotapi.Message) {
	args := msg.CommandArguments()

	if args == "" {
		b.reply(msg, "Usage: <code>/max_links &lt;1-5&gt;</code>")

		return
	}

	val, err := strconv.Atoi(args)

	if err != nil || val < 1 || val > 5 {
		b.reply(msg, "❌ Invalid value. Please provide a number between 1 and 5.")

		return
	}

	ctx := context.Background()

	if err := b.database.SaveSettingWithHistory(ctx, "max_links_per_message", val, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving max links: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Max links per message updated to <code>%d</code>.", val))
}

func (b *Bot) handleLinkCache(msg *tgbotapi.Message) {
	args := msg.CommandArguments()

	if args == "" {
		b.reply(msg, "Usage: <code>/link_cache &lt;duration&gt;</code> (e.g. <code>12h</code>, <code>24h</code>, <code>7d</code>)")

		return
	}

	durationStr := args

	if strings.HasSuffix(durationStr, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(durationStr, "d"))
		if err == nil {
			durationStr = fmt.Sprintf("%dh", days*24)
		}
	}

	_, err := time.ParseDuration(durationStr)
	if err != nil {
		b.reply(msg, "❌ Invalid duration format. Use something like <code>12h</code>, <code>24h</code>.")

		return
	}

	ctx := context.Background()

	if err := b.database.SaveSettingWithHistory(ctx, "link_cache_ttl", args, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving link cache TTL: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Link cache TTL updated to <code>%s</code>.", html.EscapeString(args)))
}

func (b *Bot) handleAdsKeywords(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	ctx := context.Background()

	if len(args) == 0 {
		var keywords []string

		if err := b.database.GetSetting(ctx, "filters_ads_keywords", &keywords); err != nil {
			b.reply(msg, "❌ Error fetching ads keywords.")

			return
		}

		if len(keywords) == 0 {
			keywords = []string{"#ad", "sponsored", "promo", "подпишись", "купи", "зарабатывай", "выигрывай"}
		}

		b.reply(msg, fmt.Sprintf("📋 <b>Ads Keywords:</b>\n<code>%s</code>\n\nUsage: <code>/adskeywords add &lt;word&gt;</code> or <code>/adskeywords remove &lt;word&gt;</code> or <code>/adskeywords clear</code>", html.EscapeString(strings.Join(keywords, ", "))))

		return
	}

	var keywords []string

	if err := b.database.GetSetting(ctx, "filters_ads_keywords", &keywords); err != nil {
		b.reply(msg, "❌ Error fetching ads keywords.")

		return
	}

	if len(keywords) == 0 {
		keywords = []string{"#ad", "sponsored", "promo", "подпишись", "купи", "зарабатывай", "выигрывай"}
	}

	switch args[0] {
	case "add":
		if len(args) < 2 {
			b.reply(msg, "Usage: <code>/adskeywords add &lt;word&gt;</code>")

			return
		}

		word := strings.ToLower(args[1])
		for _, k := range keywords {
			if k == word {
				b.reply(msg, "❌ Keyword already exists.")

				return
			}
		}

		keywords = append(keywords, word)
	case "remove":
		if len(args) < 2 {
			b.reply(msg, "Usage: <code>/adskeywords remove &lt;word&gt;</code>")

			return
		}

		word := strings.ToLower(args[1])
		newKeywords := make([]string, 0)
		found := false

		for _, k := range keywords {
			if k != word {
				newKeywords = append(newKeywords, k)
			} else {
				found = true
			}
		}

		if !found {
			b.reply(msg, "❌ Keyword not found.")

			return
		}

		keywords = newKeywords
	case "clear":
		keywords = []string{}
	default:
		b.reply(msg, "❓ Unknown command. Use <code>add</code>, <code>remove</code>, <code>clear</code> or no arguments to list.")

		return
	}

	if err := b.database.SaveSettingWithHistory(ctx, "filters_ads_keywords", keywords, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving ads keywords: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Ads keywords updated. Total: <code>%d</code>", len(keywords)))
}

func (b *Bot) handleModel(msg *tgbotapi.Message) {
	args := msg.CommandArguments()

	if args == "" {
		b.reply(msg, "Usage: <code>/model &lt;name&gt;</code> (e.g. <code>gpt-4o</code>, <code>gpt-4o-mini</code>)")

		return
	}

	ctx := context.Background()

	if err := b.database.SaveSettingWithHistory(ctx, "llm_model", args, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving LLM model: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ LLM model updated to <code>%s</code>. It will be used for the next processing batches.", html.EscapeString(args)))
}

func (b *Bot) handleListChannels(msg *tgbotapi.Message) {
	ctx := context.Background()

	channels, err := b.database.GetActiveChannels(ctx)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching channels: %s", html.EscapeString(err.Error())))

		return
	}

	if len(channels) == 0 {
		b.reply(msg, "No active channels tracked.")

		return
	}

	var sb strings.Builder

	sb.WriteString("📋 <b>Active Tracked Channels:</b>\n\n")

	for _, ch := range channels {
		identifier := fmt.Sprintf("@%s", html.EscapeString(ch.Username))
		if ch.Username == "" {
			identifier = fmt.Sprintf("ID: <code>%d</code>", ch.TGPeerID)
		}

		title := ch.Title
		if title == "" {
			title = "Pending..."
		}

		sb.WriteString(fmt.Sprintf("• %s (%s)\n", html.EscapeString(title), identifier))
		// Show weight
		weightStr := fmt.Sprintf("%.1fx", ch.ImportanceWeight)
		if ch.WeightOverride {
			weightStr += " (manual)"
		}

		sb.WriteString(fmt.Sprintf("  Weight: <code>%s</code>\n", weightStr))

		if ch.Context != "" {
			sb.WriteString(fmt.Sprintf("  <i>Context: %s</i>\n", html.EscapeString(ch.Context)))
		}

		if ch.Description != "" {
			sb.WriteString(fmt.Sprintf("  <i>Description: %s</i>\n", html.EscapeString(ch.Description)))
		}

		if ch.Category != "" || ch.Tone != "" || ch.UpdateFreq != "" {
			meta := ""
			if ch.Category != "" {
				meta += "Category: " + ch.Category + " "
			}

			if ch.Tone != "" {
				meta += "Tone: " + ch.Tone + " "
			}

			if ch.UpdateFreq != "" {
				meta += "Freq: " + ch.UpdateFreq
			}

			sb.WriteString(fmt.Sprintf("  <i>Metadata: %s</i>\n", html.EscapeString(strings.TrimSpace(meta))))
		}
	}

	sb.WriteString("\n💡 <i>Use <code>/channel weight</code> to view/set importance weight.</i>")
	b.reply(msg, sb.String())
}

func (b *Bot) handleChannelStats(msg *tgbotapi.Message) {
	ctx := context.Background()

	stats, err := b.database.GetChannelStats(ctx)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching channel stats: %s", html.EscapeString(err.Error())))

		return
	}

	channels, err := b.database.GetActiveChannels(ctx)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching channels: %s", html.EscapeString(err.Error())))

		return
	}

	channelMap := make(map[string]db.Channel)
	for _, ch := range channels {
		channelMap[ch.ID] = ch
	}

	if len(stats) == 0 {
		b.reply(msg, "No stats available yet. Statistics are calculated over the last 7 days.")

		return
	}

	var sb strings.Builder

	sb.WriteString("📈 <b>Channel Quality Metrics (Last 7 Days)</b>\n\n")

	for id, s := range stats {
		ch, ok := channelMap[id]
		name := id

		if ok {
			if ch.Username != "" {
				name = "@" + ch.Username
			} else {
				name = ch.Title
			}
		}

		sb.WriteString(fmt.Sprintf("• <b>%s</b>\n", html.EscapeString(name)))
		sb.WriteString(fmt.Sprintf("  ├ Conv. Rate: <code>%.1f%%</code>\n", s.ConversionRate))
		sb.WriteString(fmt.Sprintf("  ├ Avg Relevance: <code>%.2f</code> (σ=%.2f)\n", s.AvgRelevance, s.StddevRelevance))
		sb.WriteString(fmt.Sprintf("  └ Avg Importance: <code>%.2f</code> (σ=%.2f)\n\n", s.AvgImportance, s.StddevImportance))
	}

	b.reply(msg, sb.String())
}

func (b *Bot) handleRatings(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	days := 30
	limit := 10

	if len(args) > 0 {
		if v, err := strconv.Atoi(args[0]); err == nil && v > 0 {
			days = v
		}
	}

	if len(args) > 1 {
		if v, err := strconv.Atoi(args[1]); err == nil && v > 0 {
			limit = v
		}
	}

	ctx := context.Background()
	since := time.Now().AddDate(0, 0, -days)

	summaries, err := b.database.GetItemRatingSummary(ctx, since)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching ratings: %s", html.EscapeString(err.Error())))

		return
	}

	if len(summaries) == 0 {
		b.reply(msg, fmt.Sprintf("No item ratings in the last %d days.", days))

		return
	}

	totalGood := 0
	totalBad := 0
	totalIrrelevant := 0
	totalAll := 0

	for _, s := range summaries {
		totalGood += s.GoodCount
		totalBad += s.BadCount
		totalIrrelevant += s.IrrelevantCount
		totalAll += s.TotalCount
	}

	if limit > len(summaries) {
		limit = len(summaries)
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("⭐ <b>Item Ratings (last %d days)</b>\n\n", days))
	sb.WriteString(fmt.Sprintf("Total: <code>%d</code> (good %d | bad %d | irrelevant %d)\n\n", totalAll, totalGood, totalBad, totalIrrelevant))

	for i := 0; i < limit; i++ {
		s := summaries[i]

		name := s.ChannelID
		if s.Username != "" {
			name = "@" + s.Username
		} else if s.Title != "" {
			name = s.Title
		}

		reliability := 0.0
		if s.TotalCount > 0 {
			reliability = float64(s.GoodCount) / float64(s.TotalCount)
		}

		sb.WriteString(fmt.Sprintf("• <b>%s</b>: <code>%d</code> (g %d | b %d | i %d) rel <code>%.2f</code>\n",
			html.EscapeString(name), s.TotalCount, s.GoodCount, s.BadCount, s.IrrelevantCount, reliability))
	}

	b.reply(msg, sb.String())
}

func (b *Bot) handlePrompt(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 {
		b.reply(msg, "Usage:\n"+
			"<code>/prompt list</code>\n"+
			"<code>/prompt show &lt;summarize|narrative|cluster_summary|cluster_topic&gt; [version]</code>\n"+
			"<code>/prompt set &lt;base&gt; &lt;version&gt; &lt;text...&gt;</code>\n"+
			"<code>/prompt activate &lt;base&gt; &lt;version&gt;</code>")

		return
	}

	baseList := []string{"summarize", "narrative", "cluster_summary", "cluster_topic"}

	isValidBase := func(v string) bool {
		for _, baseName := range baseList {
			if baseName == v {
				return true
			}
		}

		return false
	}

	command := strings.ToLower(args[0])
	ctx := context.Background()

	switch command {
	case "list":
		var sb strings.Builder
		sb.WriteString("🧩 <b>Prompt Templates</b>\n\n")

		for _, baseName := range baseList {
			activeKey := fmt.Sprintf("prompt:%s:active", baseName)
			active := "v1"
			_ = b.database.GetSetting(ctx, activeKey, &active)
			sb.WriteString(fmt.Sprintf("• <b>%s</b> active: <code>%s</code>\n", html.EscapeString(baseName), html.EscapeString(active)))
		}

		b.reply(msg, sb.String())

		return
	case "show":
		if len(args) < 2 {
			b.reply(msg, "Usage: <code>/prompt show &lt;base&gt; [version]</code>")

			return
		}

		baseName := strings.ToLower(args[1])
		if !isValidBase(baseName) {
			b.reply(msg, fmt.Sprintf("Unknown base. Use: <code>%s</code>", html.EscapeString(strings.Join(baseList, ", "))))

			return
		}

		version := "v1"
		if len(args) > 2 {
			version = args[2]
		} else {
			activeKey := fmt.Sprintf("prompt:%s:active", baseName)
			_ = b.database.GetSetting(ctx, activeKey, &version)

			if version == "" {
				version = "v1"
			}
		}

		promptKey := fmt.Sprintf("prompt:%s:%s", baseName, version)

		var prompt string

		_ = b.database.GetSetting(ctx, promptKey, &prompt)
		if prompt == "" {
			b.reply(msg, fmt.Sprintf("No override found for <code>%s</code> (version <code>%s</code>). Using built-in default.", html.EscapeString(baseName), html.EscapeString(version)))

			return
		}

		escaped := html.EscapeString(prompt)

		b.reply(msg, fmt.Sprintf("Prompt <b>%s</b> (<code>%s</code>):\n<pre>%s</pre>", html.EscapeString(baseName), html.EscapeString(version), escaped))

		return
	case "set":
		if len(args) < 4 {
			b.reply(msg, "Usage: <code>/prompt set &lt;base&gt; &lt;version&gt; &lt;text...&gt;</code>")

			return
		}

		baseName := strings.ToLower(args[1])
		if !isValidBase(baseName) {
			b.reply(msg, fmt.Sprintf("Unknown base. Use: <code>%s</code>", html.EscapeString(strings.Join(baseList, ", "))))

			return
		}

		version := args[2]
		text := strings.Join(args[3:], " ")

		key := fmt.Sprintf("prompt:%s:%s", baseName, version)
		if err := b.database.SaveSettingWithHistory(ctx, key, text, msg.From.ID); err != nil {
			b.reply(msg, fmt.Sprintf("❌ Error saving prompt: %s", html.EscapeString(err.Error())))

			return
		}

		b.reply(msg, fmt.Sprintf("✅ Prompt <b>%s</b> saved as <code>%s</code>.", html.EscapeString(baseName), html.EscapeString(version)))

		return
	case "activate", "active":
		if len(args) < 3 {
			b.reply(msg, "Usage: <code>/prompt activate &lt;base&gt; &lt;version&gt;</code>")

			return
		}

		baseName := strings.ToLower(args[1])
		if !isValidBase(baseName) {
			b.reply(msg, fmt.Sprintf("Unknown base. Use: <code>%s</code>", html.EscapeString(strings.Join(baseList, ", "))))

			return
		}

		version := args[2]

		key := fmt.Sprintf("prompt:%s:active", baseName)
		if err := b.database.SaveSettingWithHistory(ctx, key, version, msg.From.ID); err != nil {
			b.reply(msg, fmt.Sprintf("❌ Error saving active version: %s", html.EscapeString(err.Error())))

			return
		}

		b.reply(msg, fmt.Sprintf("✅ Active prompt for <b>%s</b> set to <code>%s</code>.", html.EscapeString(baseName), html.EscapeString(version)))

		return
	default:
		b.reply(msg, "Usage:\n"+
			"<code>/prompt list</code>\n"+
			"<code>/prompt show &lt;base&gt; [version]</code>\n"+
			"<code>/prompt set &lt;base&gt; &lt;version&gt; &lt;text...&gt;</code>\n"+
			"<code>/prompt activate &lt;base&gt; &lt;version&gt;</code>")
	}
}

func (b *Bot) handleChannelWeight(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	ctx := context.Background()

	// No args - show usage
	if len(args) == 0 {
		b.reply(msg, "Usage:\n"+
			"<code>/channel weight @username</code> - Show current weight\n"+
			"<code>/channel weight @username 1.5</code> - Set weight (0.1-2.0)\n"+
			"<code>/channel weight @username auto</code> - Enable auto-calculation\n"+
			"<code>/channel weight @username 1.5 reason text</code> - Set weight with reason")

		return
	}

	identifier := strings.TrimPrefix(args[0], "@")

	// Check if user forgot to specify channel (e.g., "/channel weight auto" instead of "/channel weight @chan auto")
	if len(args) == 1 && (identifier == "auto" || isNumericWeight(identifier)) {
		b.reply(msg, "Missing channel identifier.\nUsage: <code>/channel weight @username</code> or <code>/channel weight @username 1.5</code>")

		return
	}

	// Just identifier - show current weight
	if len(args) == 1 {
		weight, err := b.database.GetChannelWeight(ctx, identifier)
		if err != nil {
			if strings.Contains(err.Error(), "no rows") {
				b.reply(msg, fmt.Sprintf("Channel <code>@%s</code> not found.", html.EscapeString(identifier)))
			} else {
				b.reply(msg, fmt.Sprintf("Error: %s", html.EscapeString(err.Error())))
			}

			return
		}

		var sb strings.Builder

		chanDisplay := formatChannelDisplay(weight.Username, weight.Title, identifier)
		sb.WriteString(fmt.Sprintf("<b>Channel Weight: %s</b>\n\n", chanDisplay))

		if weight.Title != "" && weight.Username != "" {
			sb.WriteString(fmt.Sprintf("Title: %s\n", html.EscapeString(weight.Title)))
		}

		sb.WriteString(fmt.Sprintf("Weight: <code>%.2f</code>", weight.ImportanceWeight))

		if weight.WeightOverride {
			sb.WriteString(" (manual override)")
		} else if weight.AutoWeightEnabled {
			sb.WriteString(" (auto)")
		}

		sb.WriteString("\n")

		if weight.WeightOverrideReason != "" {
			sb.WriteString(fmt.Sprintf("Reason: <i>%s</i>\n", html.EscapeString(weight.WeightOverrideReason)))
		}

		if weight.WeightUpdatedAt != nil {
			sb.WriteString(fmt.Sprintf("Updated: %s\n", *weight.WeightUpdatedAt))
		}

		b.reply(msg, sb.String())

		return
	}

	// Set weight or enable auto
	weightArg := args[1]

	if weightArg == "auto" {
		// Enable auto-weight: autoEnabled=true, override=false
		result, err := b.database.UpdateChannelWeight(ctx, identifier, 1.0, true, false, "", msg.From.ID)
		if err != nil {
			if strings.Contains(err.Error(), "no rows") {
				b.reply(msg, fmt.Sprintf("Channel <code>%s</code> not found.", html.EscapeString(identifier)))
			} else {
				b.reply(msg, fmt.Sprintf("Error: %s", html.EscapeString(err.Error())))
			}

			return
		}

		chanDisplay := formatChannelDisplay(result.Username, result.Title, identifier)
		b.reply(msg, fmt.Sprintf("Auto-weight enabled for %s. Weight reset to 1.0.", chanDisplay))

		return
	}

	weight, err := strconv.ParseFloat(weightArg, 32)

	if err != nil || weight < 0.1 || weight > 2.0 {
		b.reply(msg, "Invalid weight. Use a number between 0.1 and 2.0, or 'auto' to reset to default.")

		return
	}

	reason := ""

	if len(args) > 2 {
		reason = strings.Join(args[2:], " ")
	}

	// Manual weight: autoEnabled=false, override=true
	result, err := b.database.UpdateChannelWeight(ctx, identifier, float32(weight), false, true, reason, msg.From.ID)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			b.reply(msg, fmt.Sprintf("Channel <code>%s</code> not found.", html.EscapeString(identifier)))
		} else {
			b.reply(msg, fmt.Sprintf("Error: %s", html.EscapeString(err.Error())))
		}

		return
	}

	chanDisplay := formatChannelDisplay(result.Username, result.Title, identifier)
	reply := fmt.Sprintf("Weight for %s set to <code>%.2f</code>", chanDisplay, weight)

	if reason != "" {
		reply += fmt.Sprintf("\nReason: <i>%s</i>", html.EscapeString(reason))
	}

	b.reply(msg, reply)
}

// isNumericWeight checks if a string looks like a weight value (number between 0.1 and 2.0)
func isNumericWeight(s string) bool {
	f, err := strconv.ParseFloat(s, 32)
	return err == nil && f >= 0.1 && f <= 2.0
}

// formatChannelDisplay returns a display string for a channel, preferring username then title then identifier
func formatChannelDisplay(username, title, identifier string) string {
	if username != "" {
		return fmt.Sprintf("<code>@%s</code>", html.EscapeString(username))
	}

	if title != "" {
		return fmt.Sprintf("<b>%s</b>", html.EscapeString(title))
	}

	return fmt.Sprintf("<code>%s</code>", html.EscapeString(identifier))
}

func (b *Bot) handleChannelContext(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) < 2 {
		b.reply(msg, "Usage: <code>/channelcontext &lt;@username|ID&gt; &lt;context text&gt;</code>\nTo clear context: <code>/channelcontext &lt;@username|ID&gt; clear</code>")

		return
	}

	identifier := args[0]
	contextText := strings.Join(args[1:], " ")

	if strings.ToLower(contextText) == "clear" {
		contextText = ""
	}

	ctx := context.Background()
	username := strings.TrimPrefix(identifier, "@")

	if err := b.database.UpdateChannelContext(ctx, username, contextText); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error updating channel context: %s", html.EscapeString(err.Error())))

		return
	}

	if contextText == "" {
		b.reply(msg, fmt.Sprintf("✅ Context cleared for <b>%s</b>.", html.EscapeString(identifier)))
	} else {
		b.reply(msg, fmt.Sprintf("✅ Context updated for <b>%s</b>.", html.EscapeString(identifier)))
	}
}

func (b *Bot) handleFeedback(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) < 2 {
		b.reply(msg, "Usage: <code>/feedback &lt;item_id&gt; &lt;good|bad|irrelevant&gt; [comment]</code>")

		return
	}

	itemID := args[0]
	rating := strings.ToLower(args[1])

	if rating != RatingGood && rating != RatingBad && rating != RatingIrrelevant {
		b.reply(msg, "❌ Invalid rating. Use <code>good</code>, <code>bad</code>, or <code>irrelevant</code>.")

		return
	}

	feedback := ""

	if len(args) > 2 {
		feedback = strings.Join(args[2:], " ")
	}

	ctx := context.Background()

	if err := b.database.SaveItemRating(ctx, itemID, msg.From.ID, rating, feedback); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving feedback: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Feedback for item <code>%s</code> recorded as <b>%s</b>.", html.EscapeString(itemID), html.EscapeString(rating)))
}

func (b *Bot) handleChannelMetadata(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) < 4 {
		b.reply(msg, "Usage: <code>/channel metadata &lt;@username|ID&gt; &lt;category&gt; &lt;tone&gt; &lt;freq&gt; [relevance] [importance]</code>\nUse <code>-</code> to skip a field.")

		return
	}

	identifier := args[0]
	category := args[1]
	tone := args[2]
	freq := args[3]

	if category == "-" {
		category = ""
	}

	if tone == "-" {
		tone = ""
	}

	if freq == "-" {
		freq = ""
	}

	var rel, imp float64

	if len(args) > 4 && args[4] != "-" {
		rel, _ = strconv.ParseFloat(args[4], 32)
	}

	if len(args) > 5 && args[5] != "-" {
		imp, _ = strconv.ParseFloat(args[5], 32)
	}

	ctx := context.Background()
	username := strings.TrimPrefix(identifier, "@")

	if err := b.database.UpdateChannelMetadata(ctx, username, category, tone, freq, float32(rel), float32(imp)); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error updating channel metadata: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Metadata updated for channel <code>%s</code>.", html.EscapeString(identifier)))
}

func (b *Bot) handleAddChannel(msg *tgbotapi.Message) {
	args := msg.CommandArguments()

	if args == "" {
		b.reply(msg, "Usage: <code>/add &lt;@username|ID|invite_link&gt;</code>")

		return
	}

	ctx := context.Background()

	// 1. Check if it's an invite link
	if strings.Contains(args, "t.me/") {
		if err := b.database.AddChannelByInviteLink(ctx, args); err != nil {
			b.reply(msg, fmt.Sprintf("❌ Error adding channel by invite link: %s", html.EscapeString(err.Error())))

			return
		}

		b.reply(msg, "✅ Channel added by invite link. Reader will attempt to join and track it soon.")

		return
	}

	// 2. Check if it's a numeric ID
	if id, err := strconv.ParseInt(args, 10, 64); err == nil {
		if err := b.database.AddChannelByID(ctx, id); err != nil {
			b.reply(msg, fmt.Sprintf("❌ Error adding channel by ID: %s", html.EscapeString(err.Error())))

			return
		}

		b.reply(msg, fmt.Sprintf("✅ Channel ID <code>%d</code> added. Reader will start tracking it soon.", id))

		return
	}

	// 3. Fallback to username
	username := strings.TrimPrefix(args, "@")

	if err := b.database.AddChannelByUsername(ctx, username); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error adding channel by username: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Channel <code>@%s</code> added. Reader will start tracking it soon.", html.EscapeString(username)))
}

func (b *Bot) handleRemoveChannel(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.reply(msg, "Usage: <code>/remove &lt;@username|ID&gt;</code>")

		return
	}

	identifier := args[0]

	if len(args) < 2 || args[1] != "confirm" {
		b.reply(msg, fmt.Sprintf("⚠️ Are you sure you want to stop tracking channel <code>%s</code>?\nUse <code>/remove %s confirm</code> to proceed.", html.EscapeString(identifier), html.EscapeString(identifier)))

		return
	}

	ctx := context.Background()

	if err := b.database.DeactivateChannel(ctx, identifier); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error removing channel: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Channel <code>%s</code> removed.", html.EscapeString(identifier)))
}

func (b *Bot) handleFilters(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	ctx := context.Background()

	if len(args) == 0 {
		// List filters
		filters, err := b.database.GetActiveFilters(ctx)
		if err != nil {
			b.reply(msg, fmt.Sprintf("❌ Error fetching filters: %s", html.EscapeString(err.Error())))

			return
		}

		var adsEnabled bool

		_ = b.database.GetSetting(ctx, "filters_ads", &adsEnabled)

		var sb strings.Builder

		sb.WriteString("🔍 <b>Filter Management</b>\n\n")
		sb.WriteString(fmt.Sprintf("• <b>Ads filter:</b> <code>%s</code>\n", map[bool]string{true: "ON", false: "OFF"}[adsEnabled]))

		if len(filters) == 0 {
			sb.WriteString("\nNo active keyword filters. \nUsage: <code>/filters add &lt;allow|deny&gt; &lt;pattern&gt;</code> or <code>/filters remove &lt;pattern&gt;</code>")
		} else {
			sb.WriteString("\n<b>Active keyword filters:</b>\n")

			for _, f := range filters {
				sb.WriteString(fmt.Sprintf("• [%s] <code>%s</code>\n", strings.ToUpper(f.Type), html.EscapeString(f.Pattern)))
			}
		}

		b.reply(msg, sb.String())

		return
	}

	switch args[0] {
	case "add":
		if len(args) < 3 {
			b.reply(msg, "Usage: <code>/filters add &lt;allow|deny&gt; &lt;pattern&gt;</code>")

			return
		}

		fType := args[1]
		pattern := strings.Join(args[2:], " ")

		if err := b.database.AddFilter(ctx, fType, pattern); err != nil {
			b.reply(msg, fmt.Sprintf("❌ Error adding filter: %s", html.EscapeString(err.Error())))

			return
		}

		b.reply(msg, fmt.Sprintf("✅ Filter added: [%s] <code>%s</code>", strings.ToUpper(fType), html.EscapeString(pattern)))
	case "remove":
		if len(args) < 2 {
			b.reply(msg, "Usage: <code>/filters remove &lt;pattern&gt;</code>")

			return
		}

		pattern := strings.Join(args[1:], " ")

		if err := b.database.DeactivateFilter(ctx, pattern); err != nil {
			b.reply(msg, fmt.Sprintf("❌ Error removing filter: %s", html.EscapeString(err.Error())))

			return
		}

		b.reply(msg, fmt.Sprintf("✅ Filter removed: <code>%s</code>", html.EscapeString(pattern)))
	case "ads":
		if len(args) < 2 {
			b.reply(msg, "Usage: <code>/filters ads &lt;on|off&gt;</code>")

			return
		}

		enabled := args[1] == "on"

		if err := b.database.SaveSettingWithHistory(ctx, "filters_ads", enabled, msg.From.ID); err != nil {
			b.reply(msg, fmt.Sprintf("❌ Error saving ads filter setting: %s", html.EscapeString(err.Error())))

			return
		}

		b.reply(msg, fmt.Sprintf("✅ Ads filter turned <code>%s</code>.", strings.ToUpper(args[1])))
	case "mode":
		if len(args) < 2 {
			b.reply(msg, "Usage: <code>/filters mode &lt;mixed|allowlist|denylist&gt;</code>")

			return
		}

		mode := strings.ToLower(args[1])

		if mode != "mixed" && mode != "allowlist" && mode != "denylist" {
			b.reply(msg, "❌ Invalid mode. Use <code>mixed</code>, <code>allowlist</code> or <code>denylist</code>.")

			return
		}

		if err := b.database.SaveSettingWithHistory(ctx, "filters_mode", mode, msg.From.ID); err != nil {
			b.reply(msg, fmt.Sprintf("❌ Error saving filters mode: %s", html.EscapeString(err.Error())))

			return
		}

		b.reply(msg, fmt.Sprintf("✅ Filters mode set to <code>%s</code>.", mode))
	default:
		b.reply(msg, "❓ Unknown filters command. Use <code>add</code>, <code>remove</code>, <code>ads</code>, <code>mode</code> or no arguments to list.")
	}
}

func (b *Bot) handleTopics(msg *tgbotapi.Message) {
	b.handleToggleSetting(msg, "topics_enabled")
}

func (b *Bot) handleToggleSetting(msg *tgbotapi.Message, key string) {
	args := msg.CommandArguments()

	if args != "on" && args != "off" {
		// Derive command name from key (e.g., "editor_enabled" -> "editor")
		cmdName := strings.TrimSuffix(key, "_enabled")
		cmdName = strings.ReplaceAll(cmdName, "_", " ")
		b.reply(msg, fmt.Sprintf("Usage: <code>/%s &lt;on|off&gt;</code>", html.EscapeString(cmdName)))

		return
	}

	enabled := args == "on"
	ctx := context.Background()

	var current bool

	_ = b.database.GetSetting(ctx, key, &current)

	if err := b.database.SaveSettingWithHistory(ctx, key, enabled, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving %s: %s", html.EscapeString(key), html.EscapeString(err.Error())))

		return
	}

	label := cases.Title(language.English).String(strings.ReplaceAll(key, "_", " "))
	status := "ENABLED"

	if !enabled {
		status = "DISABLED"
	}

	oldStatus := "ENABLED"

	if !current {
		oldStatus = "DISABLED"
	}

	b.reply(msg, fmt.Sprintf("✅ <b>%s</b> updated.\nOld status: <code>%s</code>\nNew status: <code>%s</code>", html.EscapeString(label), oldStatus, status))
}

func (b *Bot) handleSetup(msg *tgbotapi.Message) {
	ctx := context.Background()

	var targetID int64

	_ = b.database.GetSetting(ctx, "target_chat_id", &targetID)

	channels, _ := b.database.GetActiveChannels(ctx)

	var sb strings.Builder

	sb.WriteString("🚀 <b>Getting Started with Digest Bot</b>\n\n")

	if targetID == 0 {
		sb.WriteString("1️⃣ <b>Set Target Channel</b>\n")
		sb.WriteString("First, set the channel where the bot will post digests.\n")
		sb.WriteString("Usage: <code>/target @your_channel_username</code>\n\n")
	} else {
		sb.WriteString("1️⃣ <b>Target Channel:</b> ✅ Set\n\n")
	}

	if len(channels) == 0 {
		sb.WriteString("2️⃣ <b>Add Source Channels</b>\n")
		sb.WriteString("Add some channels to track news from.\n")
		sb.WriteString("Usage: <code>/add @source_channel</code>\n\n")
	} else {
		sb.WriteString(fmt.Sprintf("2️⃣ <b>Source Channels:</b> ✅ %d added\n\n", len(channels)))
	}

	sb.WriteString("3️⃣ <b>Basic Configuration</b>\n")
	sb.WriteString("• <code>/window 60m</code> - Set digest interval\n")
	sb.WriteString("• <code>/language ru</code> - Set digest language\n\n")

	sb.WriteString("💡 <i>Tip: Use /settings to see all current values.</i>")

	b.reply(msg, sb.String())
}

func (b *Bot) handlePreview(msg *tgbotapi.Message) {
	ctx := context.Background()

	windowStr := b.cfg.DigestWindow

	if err := b.database.GetSetting(ctx, "digest_window", &windowStr); err != nil {
		b.logger.Debug().Err(err).Msg("could not get digest_window from DB")
	}

	window, err := time.ParseDuration(windowStr)
	if err != nil {
		window = time.Hour
	}

	importanceThreshold := b.cfg.ImportanceThreshold

	if err := b.database.GetSetting(ctx, "importance_threshold", &importanceThreshold); err != nil {
		b.logger.Debug().Err(err).Msg("could not get importance_threshold from DB")
	}

	now := time.Now()
	end := now
	start := now.Add(-window)

	// Create a temporary scheduler to reuse BuildDigest logic
	s := digest.New(b.cfg, b.database, b, b.llmClient, b.logger)

	text, items, _, _, err := s.BuildDigest(ctx, start, end, importanceThreshold, b.logger)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error building digest preview: %s", html.EscapeString(err.Error())))

		return
	}

	if text == "" {
		b.reply(msg, "ℹ️ No items found for the current window to include in a digest.")

		return
	}

	header := fmt.Sprintf("📝 <b>Digest Preview</b> (%d items)\n<i>This has not been posted to the target channel.</i>\n\n", len(items))
	b.reply(msg, header+text)
}

func (b *Bot) handleTone(msg *tgbotapi.Message) {
	args := strings.ToLower(msg.CommandArguments())

	if args != "professional" && args != "casual" && args != "brief" {
		b.reply(msg, "Usage: <code>/tone &lt;professional|casual|brief&gt;</code>")

		return
	}

	ctx := context.Background()

	if err := b.database.SaveSettingWithHistory(ctx, "digest_tone", args, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving tone: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Digest tone set to <code>%s</code>.", html.EscapeString(args)))
}

func (b *Bot) handleSmartModel(msg *tgbotapi.Message) {
	args := msg.CommandArguments()

	if args == "" {
		b.reply(msg, "Usage: <code>/smartmodel &lt;name&gt;</code> (e.g. <code>gpt-4o</code>)")

		return
	}

	ctx := context.Background()

	if err := b.database.SaveSettingWithHistory(ctx, "smart_llm_model", args, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving smart LLM model: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Smart LLM model updated to <code>%s</code>.", html.EscapeString(args)))
}

func (b *Bot) handleDedup(msg *tgbotapi.Message) {
	args := msg.CommandArguments()

	if args != "strict" && args != "semantic" {
		b.reply(msg, "Usage: <code>/dedup &lt;strict|semantic&gt;</code>")

		return
	}

	ctx := context.Background()

	if err := b.database.SaveSettingWithHistory(ctx, "dedup_mode", args, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving dedup mode: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Deduplication mode set to <code>%s</code>.", html.EscapeString(args)))
}

func (b *Bot) handleSettings(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	ctx := context.Background()

	if len(args) > 0 && args[0] == "reset" {
		if len(args) < 2 {
			b.reply(msg, "Usage: <code>/settings reset &lt;key&gt;</code>")

			return
		}

		key := args[1]

		if err := b.database.DeleteSettingWithHistory(ctx, key, msg.From.ID); err != nil {
			b.reply(msg, fmt.Sprintf("❌ Error resetting setting: %s", html.EscapeString(err.Error())))

			return
		}

		b.reply(msg, fmt.Sprintf("✅ Setting <code>%s</code> has been reset to default (env var value).", html.EscapeString(key)))

		return
	}

	dbSettings, err := b.database.GetAllSettings(ctx)
	if err != nil {
		b.reply(msg, fmt.Sprintf("Error fetching settings: %s", html.EscapeString(err.Error())))

		return
	}

	var sb strings.Builder

	sb.WriteString("⚙️ <b>Current Settings:</b>\n\n")

	// Define all settings we care about with their keys, titles and defaults
	type settingDef struct {
		key   string
		title string
		def   interface{}
	}

	settings := []settingDef{
		{"target_chat_id", "Target Chat ID", b.cfg.TargetChatID},
		{"digest_window", "Digest Window", b.cfg.DigestWindow},
		{"relevance_threshold", "Relevance Threshold", b.cfg.RelevanceThreshold},
		{"importance_threshold", "Importance Threshold", b.cfg.ImportanceThreshold},
		{"llm_model", "Primary LLM Model", b.cfg.LLMModel},
		{"smart_llm_model", "Smart LLM Model", "not set"},
		{"digest_language", "Digest Language", "default (en)"},
		{"digest_tone", "Digest Tone", "professional"},
		{"dedup_mode", "Deduplication Mode", "semantic"},
		{"normalize_scores", "Normalize Scores", false},
		{"topics_enabled", "Topics Grouping", true},
		{"editor_enabled", "Editor-in-Chief", false},
		{"tiered_importance_enabled", "Tiered Importance", false},
		{"vision_routing_enabled", "Vision Routing", false},
		{"consolidated_clusters_enabled", "Consolidated Clusters", false},
		{"editor_detailed_items", "Editor Detailed Items", true},
		{"filters_ads", "Ads Filter", false},
		{"filters_min_length", "Min Message Length", 20},
		{"filters_skip_forwards", "Skip Forwards", false},
		{"filters_ads_keywords", "Ads Keywords Count", 0},
		{"admin_ids", "Additional Admins", "none"},
	}

	for _, s := range settings {
		val, ok := dbSettings[s.key]
		if !ok {
			val = s.def
		}

		if s.key == "filters_ads_keywords" {
			if kwArr, ok := val.([]interface{}); ok {
				val = len(kwArr)
			} else if kwArr, ok := val.([]string); ok {
				val = len(kwArr)
			}
		}

		sb.WriteString(fmt.Sprintf("• <b>%s:</b> <code>%v</code>\n", s.title, html.EscapeString(fmt.Sprintf("%v", val))))
	}

	sb.WriteString(fmt.Sprintf("• <b>Static Admins:</b> <code>%v</code>\n", html.EscapeString(fmt.Sprintf("%v", b.cfg.AdminIDs))))
	sb.WriteString("\n💡 <i>Use <code>/settings reset &lt;key&gt;</code> to return a setting to its default environment value.</i>")

	b.reply(msg, sb.String())
}

func (b *Bot) handleHelp(msg *tgbotapi.Message) {
	b.reply(msg, "👋 <b>Welcome to Telegram Digest Bot!</b>\n\n"+
		"I help you reduce noise by summarizing news from multiple Telegram channels into a single digest.\n\n"+
		"🚀 <b>Getting Started</b>\n"+
		"• Use <code>/setup</code> for a guided configuration wizard.\n"+
		"• Use <code>/status</code> to check system health and backlog.\n\n"+
		"📋 <b>Channel Management</b> (<code>/channel</code>)\n"+
		"• <code>/channel add &lt;id|@user|link&gt;</code> - Track a new channel\n"+
		"• <code>/channel remove &lt;id|@user&gt;</code> - Stop tracking\n"+
		"• <code>/channel list</code> - List all tracked channels\n"+
		"• <code>/channel context &lt;id&gt; &lt;text&gt;</code> - Set channel context\n"+
		"• <code>/channel weight &lt;@user&gt; [0.1-2.0|auto]</code> - Get/set importance weight\n\n"+
		"🔍 <b>Channel Discovery</b> (<code>/discover</code>)\n"+
		"• <code>/discover</code> - View pending discovered channels\n"+
		"• <code>/discover approve @channel</code> - Add channel to tracking\n"+
		"• <code>/discover reject @channel</code> - Reject channel\n"+
		"• <code>/discover stats</code> - Discovery statistics\n\n"+
		"🔍 <b>Filters</b> (<code>/filter</code>)\n"+
		"• <code>/filter list</code> - View active filters\n"+
		"• <code>/filter add &lt;allow|deny&gt; &lt;word&gt;</code> - Filter by keyword\n"+
		"• <code>/filter ads &lt;on|off&gt;</code> - Toggle heuristic ads filter\n"+
		"• <code>/filter mode &lt;mixed|allow|deny&gt;</code> - Set filtering mode\n"+
		"• <code>/filter keywords</code> - Manage ad keywords\n"+
		"• <code>/filter min_length &lt;n&gt;</code> - Min message length\n\n"+
		"⚙️ <b>Configuration</b> (<code>/config</code>)\n"+
		"• <code>/config target &lt;id|@user&gt;</code> - Set digest destination\n"+
		"• <code>/config window &lt;duration&gt;</code> - Set digest interval (e.g., 60m)\n"+
		"• <code>/config language &lt;code&gt;</code> - Set digest language (e.g., ru)\n"+
		"• <code>/config tone &lt;professional|casual|brief&gt;</code> - Set digest tone\n"+
		"• <code>/config relevance &lt;0-1&gt;</code> - Set relevance threshold\n"+
		"• <code>/config reset &lt;key&gt;</code> - Restore default setting\n\n"+
		"🧠 <b>AI &amp; Features</b> (<code>/ai</code>)\n"+
		"• <code>/ai model &lt;name&gt;</code> - Set primary LLM model\n"+
		"• <code>/ai tone &lt;professional|casual|brief&gt;</code> - Set digest tone\n"+
		"• <code>/ai prompt</code> - Manage prompt templates\n"+
		"• <code>/ai editor &lt;on|off&gt;</code> - Toggle narrative overview\n"+
		"• <code>/ai vision &lt;on|off&gt;</code> - Toggle image analysis\n"+
		"• <code>/ai consolidated &lt;on|off&gt;</code> - Merge similar stories\n"+
		"• <code>/preview</code> - See what the next digest will look like\n\n"+
		"🛠 <b>System</b> (<code>/system</code>)\n"+
		"• <code>/channel stats</code> - Channel quality metrics (last 7 days)\n"+
		"• <code>/ratings [days] [limit]</code> - Item rating summary\n"+
		"• <code>/system status</code> - Detailed system health\n"+
		"• <code>/system settings</code> - View all configuration overrides\n"+
		"• <code>/system errors</code> - Review processing failures\n"+
		"• <code>/system retry</code> - Requeue failed items\n\n"+
		"<i>Use <code>/settings</code> to see all current values at once.</i>")
}

func (b *Bot) handleErrors(msg *tgbotapi.Message) {
	ctx := context.Background()

	errors, err := b.database.GetRecentErrors(ctx, 10)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching errors: %s", html.EscapeString(err.Error())))

		return
	}

	if len(errors) == 0 {
		b.reply(msg, "✅ No recent processing errors found.")

		return
	}

	var sb strings.Builder

	sb.WriteString("⚠️ <b>Recent Processing Errors:</b>\n\n")

	for _, e := range errors {
		sb.WriteString(fmt.Sprintf("• <b>Channel:</b> %s\n", html.EscapeString(e.SourceChannel)))
		sb.WriteString(fmt.Sprintf("  <b>Error:</b> %s\n", b.humanizeError(e.ErrorJSON)))
		sb.WriteString(fmt.Sprintf("  <b>Time:</b> <code>%s</code>\n", e.CreatedAt.Format("2006-01-02 15:04:05")))
		sb.WriteString(fmt.Sprintf("  %s | /retry_%s\n\n", FormatLink(e.SourceChannel, e.SourceChannelID, e.SourceMsgID, "[View Message]"), strings.ReplaceAll(e.ID, "-", "")))
	}

	b.reply(msg, sb.String())
}

func (b *Bot) humanizeError(errJSON []byte) string {
	var data map[string]string

	if err := json.Unmarshal(errJSON, &data); err != nil {
		return "<code>" + html.EscapeString(string(errJSON)) + "</code>"
	}

	rawErr := data["error"]

	switch {
	case strings.Contains(rawErr, "empty summary"):
		return "🤖 LLM could not generate a summary for this message."
	case strings.Contains(rawErr, "failed to save item"):
		return "💾 Database error while saving processed item."
	case strings.Contains(rawErr, "rate limiter"):
		return "⏳ Too many requests to LLM provider. Retrying later."
	case strings.Contains(rawErr, "circuit breaker"):
		return "🔌 LLM provider is currently unavailable (circuit breaker open)."
	default:
		return "<code>" + html.EscapeString(rawErr) + "</code>"
	}
}

func (b *Bot) handleHistory(msg *tgbotapi.Message) {
	ctx := context.Background()

	history, err := b.database.GetRecentSettingHistory(ctx, 20)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching history: %s", html.EscapeString(err.Error())))

		return
	}

	if len(history) == 0 {
		b.reply(msg, "📋 No setting history found.")

		return
	}

	text := "📋 <b>Recent Setting Changes:</b>\n\n"

	for _, h := range history {
		text += fmt.Sprintf("• <b>%s</b> changed by <code>%d</code>\n", html.EscapeString(h.Key), h.ChangedBy)

		text += fmt.Sprintf("  🕒 %s\n", h.ChangedAt.Format("2006-01-02 15:04:05"))
		if h.NewValue == "" {
			text += "  🗑️ <i>Deleted/Reset</i>\n"
		} else {
			oldVal := h.OldValue
			if oldVal == "" {
				oldVal = "<i>(none)</i>"
			}

			text += fmt.Sprintf("  📥 Old: <code>%s</code>\n", html.EscapeString(oldVal))
			text += fmt.Sprintf("  📤 New: <code>%s</code>\n", html.EscapeString(h.NewValue))
		}

		text += "\n"
	}

	text += "\n💡 <i>Use <code>/settings reset &lt;key&gt;</code> to return a setting to its default environment value.</i>"
	text += "\n💡 <i>Use <code>/settings reset &lt;key&gt;</code> to return a setting to its default value.</i>"
	b.reply(msg, text)
}

func (b *Bot) handleRetry(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	ctx := context.Background()

	if len(args) == 0 {
		errors, _ := b.database.GetRecentErrors(ctx, 1000)

		if len(errors) == 0 {
			b.reply(msg, "✅ No failed items found to retry.")

			return
		}

		b.reply(msg, fmt.Sprintf("⚠️ <code>%d</code> failed items found. Are you sure you want to requeue all of them?\nUse <code>/retry confirm</code> to proceed.", len(errors)))

		return
	}

	if args[0] == "confirm" {
		if err := b.database.RetryFailedItems(ctx); err != nil {
			b.reply(msg, fmt.Sprintf("❌ Error retrying items: %s", html.EscapeString(err.Error())))

			return
		}

		b.reply(msg, "✅ All failed items have been requeued for processing.")

		return
	}

	// Support both /retry ID and /retry_ID
	id := strings.TrimPrefix(args[0], "_")

	if err := b.database.RetryItem(ctx, id); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error retrying item %s: %s", html.EscapeString(id), html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Item <code>%s</code> has been requeued.", html.EscapeString(id)))
}

func (b *Bot) handleDiscoverNamespace(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	ctx := context.Background()

	if len(args) == 0 {
		// Show pending discoveries
		b.handleDiscoverList(msg)

		return
	}

	subcommand := args[0]

	switch subcommand {
	case "approve":
		if len(args) < 2 {
			b.reply(msg, "Usage: <code>/discover approve &lt;@username&gt;</code>")

			return
		}

		b.handleDiscoverApprove(ctx, msg, args[1])
	case "reject":
		if len(args) < 2 {
			b.reply(msg, "Usage: <code>/discover reject &lt;@username&gt;</code>")

			return
		}

		b.handleDiscoverReject(ctx, msg, args[1])
	case "stats":
		b.handleDiscoverStats(msg)
	default:
		b.reply(msg, fmt.Sprintf("❓ Unknown discover subcommand: <code>%s</code>. Use <code>approve</code>, <code>reject</code>, or <code>stats</code>.", html.EscapeString(subcommand)))
	}
}

func (b *Bot) handleDiscoverList(msg *tgbotapi.Message) {
	ctx := context.Background()

	discoveries, err := b.database.GetPendingDiscoveries(ctx, 15)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching discoveries: %s", html.EscapeString(err.Error())))

		return
	}

	if len(discoveries) == 0 {
		b.reply(msg, "📋 No pending channel discoveries. Channels are discovered from forwards, t.me links, and @mentions in tracked channels.")

		return
	}

	var sb strings.Builder

	sb.WriteString("🔍 <b>Pending Channel Discoveries</b>\n\n")

	for _, d := range discoveries {
		identifier := ""
		if d.Username != "" {
			identifier = "@" + d.Username
		} else if d.TGPeerID != 0 {
			identifier = fmt.Sprintf("ID:%d", d.TGPeerID)
		} else if d.InviteLink != "" {
			identifier = "[invite link]"
		}

		title := d.Title
		if title == "" {
			title = "Unknown"
		}

		sb.WriteString(fmt.Sprintf("• <b>%s</b> (%s)\n", html.EscapeString(title), html.EscapeString(identifier)))

		// Build info line with engagement if available
		infoLine := fmt.Sprintf("  Source: %s | Seen: %dx", d.SourceType, d.DiscoveryCount)
		if d.MaxViews > 0 || d.MaxForwards > 0 {
			infoLine += fmt.Sprintf(" | Engagement: %dv/%df", d.MaxViews, d.MaxForwards)
		}

		infoLine += fmt.Sprintf(" | Last: %s\n\n", d.LastSeenAt.Format("Jan 02"))
		sb.WriteString(infoLine)
	}

	sb.WriteString("\n💡 <i>Use <code>/discover approve @username</code> or <code>/discover reject @username</code></i>")

	// Build inline keyboard with approve/reject buttons for username-based discoveries
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, d := range discoveries {
		if d.Username != "" {
			row := tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ "+d.Username, "discover:approve:"+d.Username),
				tgbotapi.NewInlineKeyboardButtonData("❌ "+d.Username, "discover:reject:"+d.Username),
			)
			rows = append(rows, row)
		}
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, sb.String())
	reply.ParseMode = tgbotapi.ModeHTML

	if len(rows) > 0 {
		reply.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	}

	if _, err := b.api.Send(reply); err != nil {
		b.logger.Error().Err(err).Msg("failed to send discover list")
	}
}

func (b *Bot) handleDiscoverApprove(ctx context.Context, msg *tgbotapi.Message, username string) {
	username = strings.TrimPrefix(username, "@")

	if err := b.database.ApproveDiscovery(ctx, username, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error approving channel: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Channel <code>@%s</code> approved and added to active tracking.", html.EscapeString(username)))
}

func (b *Bot) handleDiscoverReject(ctx context.Context, msg *tgbotapi.Message, username string) {
	username = strings.TrimPrefix(username, "@")

	if err := b.database.RejectDiscovery(ctx, username, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error rejecting channel: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Channel <code>@%s</code> rejected. It will not appear in discoveries again.", html.EscapeString(username)))
}

func (b *Bot) handleDiscoverStats(msg *tgbotapi.Message) {
	ctx := context.Background()

	stats, err := b.database.GetDiscoveryStats(ctx)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching discovery stats: %s", html.EscapeString(err.Error())))

		return
	}

	var sb strings.Builder

	sb.WriteString("📊 <b>Channel Discovery Statistics</b>\n\n")
	sb.WriteString(fmt.Sprintf("• <b>Pending:</b> <code>%d</code>\n", stats.PendingCount))

	if stats.UnresolvedCount > 0 {
		sb.WriteString(fmt.Sprintf("• <b>Unresolved:</b> <code>%d</code> <i>(peer ID only)</i>\n", stats.UnresolvedCount))
	}

	sb.WriteString(fmt.Sprintf("• <b>Rejected:</b> <code>%d</code>\n", stats.RejectedCount))
	sb.WriteString(fmt.Sprintf("• <b>Added:</b> <code>%d</code>\n", stats.AddedCount))
	sb.WriteString(fmt.Sprintf("• <b>Total Channels:</b> <code>%d</code>\n", stats.TotalCount))
	sb.WriteString(fmt.Sprintf("• <b>Total Discovery Events:</b> <code>%d</code>\n", stats.TotalDiscoveries))

	b.reply(msg, sb.String())
}

func (b *Bot) handleDiscoverCallback(query *tgbotapi.CallbackQuery) {
	parts := strings.Split(query.Data, ":")

	if len(parts) != 3 {
		return
	}

	action := parts[1] // "approve" or "reject"
	username := parts[2]
	ctx := context.Background()

	var callbackText string

	var err error

	switch action {
	case "approve":
		err = b.database.ApproveDiscovery(ctx, username, query.From.ID)
		if err == nil {
			callbackText = fmt.Sprintf("✅ @%s approved and added to tracking", username)
		}
	case "reject":
		err = b.database.RejectDiscovery(ctx, username, query.From.ID)
		if err == nil {
			callbackText = fmt.Sprintf("❌ @%s rejected", username)
		}
	default:
		return
	}

	if err != nil {
		callbackText = fmt.Sprintf("Error: %s", err.Error())
		b.logger.Error().Err(err).Str("action", action).Str("username", username).Msg("discover callback failed")
	}

	callback := tgbotapi.NewCallback(query.ID, callbackText)
	callback.ShowAlert = true

	if _, err := b.api.Request(callback); err != nil {
		b.logger.Error().Err(err).Msg("failed to send callback response")
	}
}
