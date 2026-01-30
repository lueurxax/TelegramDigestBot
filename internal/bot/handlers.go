package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/lueurxax/telegram-digest-bot/internal/output/digest"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/htmlutils"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/schedule"
	"github.com/lueurxax/telegram-digest-bot/internal/research"
	"github.com/lueurxax/telegram-digest-bot/internal/storage"
)

func (b *Bot) handleThreshold(ctx context.Context, msg *tgbotapi.Message, key string) {
	args := msg.CommandArguments()
	cmdName := strings.TrimSuffix(key, "_threshold")
	label := cases.Title(language.English).String(strings.ReplaceAll(key, "_", " "))

	var current float32

	_ = b.database.GetSetting(ctx, key, &current) //nolint:errcheck // best-effort read

	if args == "" {
		hint := "higher = stricter filtering"
		b.reply(msg, fmt.Sprintf(`📊 <b>%s</b>

Current value: <code>%.2f</code>
Range: 0.0 - 1.0 (%s)

Usage: <code>/%s &lt;0.0-1.0&gt;</code>`, html.EscapeString(label), current, hint, html.EscapeString(cmdName)))

		return
	}

	val, err := strconv.ParseFloat(args, 32)

	if err != nil || val < 0 || val > 1 {
		b.reply(msg, "❌ Invalid value. Please provide a number between 0.0 and 1.0.")

		return
	}

	if err := b.database.SaveSettingWithHistory(ctx, key, float32(val), msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf(ErrSavingFmt, html.EscapeString(key), html.EscapeString(err.Error())))

		return
	}

	direction := "more items will pass"
	if val > float64(current) {
		direction = "fewer items will pass"
	}

	b.reply(msg, fmt.Sprintf("✅ <b>%s</b> updated: <code>%.2f</code> → <code>%.2f</code>\n\n💡 %s", html.EscapeString(label), current, val, direction))
}

func (b *Bot) handleStatus(ctx context.Context, msg *tgbotapi.Message) {
	backlog, _ := b.database.GetBacklogCount(ctx)                    //nolint:errcheck // best-effort read
	activeChannels, _ := b.database.CountActiveChannels(ctx)         //nolint:errcheck // best-effort read
	recentChannels, _ := b.database.CountRecentlyActiveChannels(ctx) //nolint:errcheck // best-effort read
	readyItems, _ := b.database.CountReadyItems(ctx)                 //nolint:errcheck // best-effort read
	lastDigest, _ := b.database.GetLastPostedDigest(ctx)             //nolint:errcheck // best-effort read

	var sb strings.Builder

	sb.WriteString("📊 <b>System Status</b>\n\n")
	sb.WriteString(fmt.Sprintf("• <b>Active Channels:</b> <code>%d</code>\n", activeChannels))
	sb.WriteString(fmt.Sprintf("• <b>Channels with messages (24h):</b> <code>%d</code>\n", recentChannels))
	sb.WriteString(fmt.Sprintf("• <b>Message Backlog:</b> <code>%d</code>\n", backlog))
	sb.WriteString(fmt.Sprintf("• <b>Items ready for digest:</b> <code>%d</code>\n", readyItems))

	if lastDigest != nil {
		sb.WriteString(fmt.Sprintf("• <b>Last Digest:</b> <code>%s</code>\n", lastDigest.PostedAt.Format(DateTimeFormat)))
		sb.WriteString(fmt.Sprintf("  <i>Window: %s - %s</i>\n", lastDigest.Start.Format(TimeFormat), lastDigest.End.Format(TimeFormat)))
	} else {
		sb.WriteString("• <b>Last Digest:</b> <code>None</code>\n")
	}

	b.reply(msg, sb.String())
}

func (b *Bot) handleChannelNamespace(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.reply(msg, `📺 <b>Channel Management</b>

<b>Commands:</b>
• <code>/channel add @user</code> - Add channel to tracking
• <code>/channel remove @user</code> - Remove channel
• <code>/channel list</code> - List tracked channels
• <code>/channel metadata @user ...</code> - Set category/tone
• <code>/channel weight @user</code> - View/set importance weight
• <code>/channel relevance @user</code> - View/set auto-relevance
• <code>/channel stats</code> - Channel quality metrics`)

		return
	}

	subcommand := args[0]
	newMsg := prepareSubcommandMessage(msg, subcommand, args)

	switch subcommand {
	case CmdAdd:
		b.handleAddChannel(ctx, &newMsg)
	case CmdRemove:
		b.handleRemoveChannel(ctx, &newMsg)
	case CmdList:
		b.handleListChannels(ctx, &newMsg)
	case "metadata":
		b.handleChannelMetadata(ctx, &newMsg)
	case SubCmdStats:
		b.handleChannelStats(ctx, &newMsg)
	case "weight":
		b.handleChannelWeight(ctx, &newMsg)
	case CmdRelevance:
		b.handleChannelRelevance(ctx, &newMsg)
	default:
		b.reply(msg, fmt.Sprintf("❓ Unknown subcommand: <code>%s</code>\n\n💡 Run <code>/channel</code> to see available commands.", html.EscapeString(subcommand)))
	}
}

func (b *Bot) handleFilterNamespace(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.handleFilters(ctx, msg)

		return
	}

	subcommand := args[0]
	newMsg := prepareSubcommandMessage(msg, subcommand, args)

	switch subcommand {
	case CmdList:
		b.handleFiltersList(ctx, &newMsg)
	case CmdAdd, CmdRemove, SubCmdAds, SubCmdMode:
		b.handleFilters(ctx, msg)
	case "keywords":
		b.handleAdsKeywords(ctx, &newMsg)
	case "min_length", "minlength":
		b.handleMinLength(ctx, &newMsg)
	case "skip_forwards", "skipforwards":
		b.handleToggleSetting(ctx, &newMsg, "filters_skip_forwards")
	default:
		b.reply(msg, fmt.Sprintf("❓ Unknown subcommand: <code>%s</code>\n\n💡 Run <code>/filter</code> to see current filters, or use <code>add</code>, <code>remove</code>, <code>ads</code>, <code>mode</code>.", html.EscapeString(subcommand)))
	}
}

func (b *Bot) handleConfigNamespace(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.reply(msg, `⚙️ <b>Configuration</b>

<b>Output:</b>
• <code>/config target @channel</code> - Set digest target
• <code>/config window 6h</code> - Set digest interval
• <code>/config language en</code> - Set language
• <code>/config tone casual</code> - Set tone

<b>Thresholds:</b>
• <code>/config relevance 0.5</code> - Min relevance (0-1, higher = stricter)
• <code>/config importance 0.3</code> - Min importance (0-1, higher = stricter)

<b>Links:</b>
• <code>/config links on</code> - Enable link enrichment
• <code>/config maxlinks 3</code> - Max links per message

<b>Reset:</b>
• <code>/config reset &lt;key&gt;</code> - Reset setting to default`)

		return
	}

	subcommand := args[0]
	newMsg := prepareSubcommandMessage(msg, subcommand, args)

	if !b.routeConfigSubcommand(ctx, &newMsg, subcommand) {
		b.reply(msg, fmt.Sprintf("❓ Unknown subcommand: <code>%s</code>\n\n💡 Run <code>/config</code> to see available settings.", html.EscapeString(subcommand)))
	}
}

func (b *Bot) routeConfigSubcommand(ctx context.Context, msg *tgbotapi.Message, subcommand string) bool {
	handlers := map[string]func(){
		"links":      func() { b.handleToggleSetting(ctx, msg, "link_enrichment_enabled") },
		"max_links":  func() { b.handleMaxLinks(ctx, msg) },
		"maxlinks":   func() { b.handleMaxLinks(ctx, msg) },
		"link_cache": func() { b.handleLinkCache(ctx, msg) },
		"linkcache":  func() { b.handleLinkCache(ctx, msg) },
		"target":     func() { b.handleTarget(ctx, msg) },
		"window":     func() { b.handleWindow(ctx, msg) },
		"schedule":   func() { b.handleSchedule(ctx, msg) },
		"language":   func() { b.handleLanguage(ctx, msg) },
		CmdTone:      func() { b.handleTone(ctx, msg) },
		"relevance":  func() { b.handleThreshold(ctx, msg, SettingRelevanceThreshold) },
		"importance": func() { b.handleThreshold(ctx, msg, SettingImportanceThreshold) },
		"discovery_min_seen": func() {
			args := strings.Fields(msg.Text)
			b.handleDiscoverMinSeen(ctx, msg, args)
		},
		"discovery_min_engagement": func() {
			args := strings.Fields(msg.Text)
			b.handleDiscoverMinEngagement(ctx, msg, args)
		},
		"reset": func() { b.handleSettings(ctx, msg) },
	}

	if handler, ok := handlers[subcommand]; ok {
		handler()

		return true
	}

	return false
}

func (b *Bot) handleAINamespace(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.reply(msg, `🤖 <b>AI Settings</b>

<b>Features (on/off):</b>
• <code>/ai editor on</code> - Editor-in-chief
• <code>/ai tiered on</code> - Tiered importance
• <code>/ai vision on</code> - Vision routing
• <code>/ai topics on</code> - Topic grouping
• <code>/ai consolidated on</code> - Cluster consolidation
• <code>/ai details on</code> - Detailed items

<b>Other:</b>
• <code>/ai tone casual</code> - Set digest tone
• <code>/ai dedup semantic</code> - Dedup mode (strict/semantic)
• <code>/ai prompt list</code> - Manage prompts`)

		return
	}

	subcommand := args[0]
	newMsg := prepareSubcommandMessage(msg, subcommand, args)

	if !b.routeAISubcommand(ctx, &newMsg, subcommand) {
		b.reply(msg, fmt.Sprintf("❓ Unknown subcommand: <code>%s</code>\n\n💡 Run <code>/ai</code> to see available AI settings.", html.EscapeString(subcommand)))
	}
}

func (b *Bot) routeAISubcommand(ctx context.Context, msg *tgbotapi.Message, subcommand string) bool {
	handlers := map[string]func(){
		"prompt": func() { b.handlePrompt(ctx, msg) },
		CmdTone:  func() { b.handleTone(ctx, msg) },
		"topics": func() { b.handleTopics(ctx, msg) },
		"dedup":  func() { b.handleDedup(ctx, msg) },
	}

	if handler, ok := handlers[subcommand]; ok {
		handler()

		return true
	}

	toggleSettings := map[string]string{
		"editor":        "editor_enabled",
		"tiered":        "tiered_importance_enabled",
		"vision":        "vision_routing_enabled",
		"visionrouting": "vision_routing_enabled",
		"consolidated":  "consolidated_clusters_enabled",
		"normalize":     "normalize_scores",
		"details":       "editor_detailed_items",
		"editordetails": "editor_detailed_items",
	}

	if settingKey, ok := toggleSettings[subcommand]; ok {
		b.handleToggleSetting(ctx, msg, settingKey)

		return true
	}

	return false
}

func (b *Bot) handleSystemNamespace(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.reply(msg, `🔧 <b>System Diagnostics</b>

<b>Commands:</b>
• <code>/system status</code> - System health dashboard
• <code>/system settings</code> - Show all settings
• <code>/system history</code> - Recent setting changes
• <code>/system errors</code> - Recent processing errors
• <code>/system retry</code> - Retry failed items
• <code>/system scores</code> - Item importance scores
• <code>/system factcheck</code> - Fact check status`)

		return
	}

	subcommand := args[0]
	newMsg := prepareSubcommandMessage(msg, subcommand, args)

	switch subcommand {
	case "status":
		b.handleStatus(ctx, &newMsg)
	case "settings":
		b.handleSettings(ctx, &newMsg)
	case "history":
		b.handleHistory(ctx, &newMsg)
	case "errors":
		b.handleErrors(ctx, &newMsg)
	case "retry":
		b.handleRetry(ctx, &newMsg)
	case CmdScores:
		b.handleScores(ctx, &newMsg)
	case CmdFactCheck:
		b.handleFactCheck(ctx, &newMsg)
	default:
		b.reply(msg, fmt.Sprintf("❓ Unknown subcommand: <code>%s</code>\n\n💡 Run <code>/system</code> to see available diagnostics.", html.EscapeString(subcommand)))
	}
}

func (b *Bot) handleTarget(ctx context.Context, msg *tgbotapi.Message) {
	args := msg.CommandArguments()

	if args == "" {
		b.reply(msg, "Usage: <code>/target &lt;channel_id or @username&gt;</code>")

		return
	}

	chatID, chat, errMsg := b.resolveTargetChat(args)
	if errMsg != "" {
		b.reply(msg, errMsg)

		return
	}

	if errMsg := b.verifyTargetChatPermissions(chatID, chat); errMsg != "" {
		b.reply(msg, errMsg)

		return
	}

	if err := b.database.SaveSettingWithHistory(ctx, SettingTargetChatID, chatID, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving target chat ID: %s", html.EscapeString(err.Error())))

		return
	}

	// Clear any previous digest errors so the scheduler can retry with the new target
	if err := b.database.ClearDigestErrors(ctx); err != nil {
		b.logger.Warn().Err(err).Msg("failed to clear digest errors after target update")
	}

	b.reply(msg, fmt.Sprintf("✅ Target chat updated to <code>%d</code> (<b>%s</b>). A confirmation message has been sent to that channel.", chatID, html.EscapeString(chat.Title)))
}

func (b *Bot) resolveTargetChat(args string) (int64, tgbotapi.Chat, string) {
	if strings.HasPrefix(args, "@") {
		return b.resolveTargetChatByUsername(args)
	}

	return b.resolveTargetChatByID(args)
}

func (b *Bot) resolveTargetChatByUsername(args string) (int64, tgbotapi.Chat, string) {
	chat, err := b.api.GetChat(tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{SuperGroupUsername: args}})
	if err != nil {
		username := strings.TrimPrefix(args, "@")

		chat, err = b.api.GetChat(tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{SuperGroupUsername: username}})
		if err != nil {
			return 0, tgbotapi.Chat{}, fmt.Sprintf("❌ Could not find chat %s: %s. Make sure the bot is an administrator in the channel.", html.EscapeString(args), html.EscapeString(err.Error()))
		}
	}

	return chat.ID, chat, ""
}

func (b *Bot) resolveTargetChatByID(args string) (int64, tgbotapi.Chat, string) {
	chatID, err := strconv.ParseInt(args, 10, 64)
	if err != nil {
		return 0, tgbotapi.Chat{}, "❌ Invalid channel ID. It should be a number (don't forget the <code>-100</code> prefix for channels) or a <code>@username</code>."
	}

	chat, err := b.api.GetChat(tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: chatID}})
	if err == nil {
		return chatID, chat, ""
	}

	if chatID <= 0 {
		return 0, tgbotapi.Chat{}, fmt.Sprintf("❌ Could not find chat %d: %s. Make sure the bot is added to the chat.", chatID, html.EscapeString(err.Error()))
	}

	altID, _ := strconv.ParseInt("-100"+strconv.FormatInt(chatID, 10), 10, 64) //nolint:errcheck // concatenation always valid

	chat, errAlt := b.api.GetChat(tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: altID}})
	if errAlt != nil {
		return 0, tgbotapi.Chat{}, fmt.Sprintf("❌ Could not find chat %d (nor %d): %s. Make sure the bot is added to the chat.", chatID, altID, html.EscapeString(errAlt.Error()))
	}

	return altID, chat, ""
}

func (b *Bot) verifyTargetChatPermissions(chatID int64, chat tgbotapi.Chat) string {
	testMsg := tgbotapi.NewMessage(chatID, "✅ This channel has been set as the target for digest posts.")

	if _, err := b.api.Send(testMsg); err != nil {
		return fmt.Sprintf("❌ Found chat <b>%s</b> but could not send a message to it: %s. Make sure the bot is an administrator with permission to post messages.", html.EscapeString(chat.Title), html.EscapeString(err.Error()))
	}

	return ""
}

func (b *Bot) handleWindow(ctx context.Context, msg *tgbotapi.Message) {
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

	if err := b.database.SaveSettingWithHistory(ctx, SettingDigestWindow, args, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving digest window: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Digest window updated to <code>%s</code>.", html.EscapeString(args)))
}

func (b *Bot) handleSchedule(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 {
		b.reply(msg, "Usage: <code>/schedule timezone &lt;IANA&gt;</code> | <code>/schedule weekdays times &lt;HH:00,...&gt;</code> | <code>/schedule weekdays hourly &lt;HH:00-HH:00&gt;</code> | <code>/schedule weekends hourly &lt;HH:00-HH:00&gt;</code> | <code>/schedule preview [count]</code> | <code>/schedule clear</code> | <code>/schedule show</code>")

		return
	}

	subcommand := strings.ToLower(args[0])

	switch subcommand {
	case SubCmdClear:
		b.handleScheduleClear(ctx, msg)
	case SubCmdPreview:
		b.handleSchedulePreview(ctx, msg, args)
	case SubCmdShow:
		b.reply(msg, b.formatDigestSchedule(ctx))
	case "timezone":
		b.handleScheduleTimezone(ctx, msg, args)
	case SubCmdWeekdays, SubCmdWeekends:
		b.handleScheduleDayGroup(ctx, msg, subcommand, args)
	default:
		b.reply(msg, fmt.Sprintf("❓ Unknown subcommand: <code>%s</code>\n\n💡 Use <code>/schedule show</code> to see current schedule, or <code>weekdays</code>, <code>weekends</code>, <code>timezone</code>.", html.EscapeString(subcommand)))
	}
}

func (b *Bot) handleScheduleTimezone(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) < 2 {
		b.reply(msg, "Usage: <code>/schedule timezone &lt;IANA&gt;</code> (e.g. <code>Europe/Kyiv</code>)")

		return
	}

	sched := b.loadDigestSchedule(ctx)
	sched.Timezone = args[1]

	b.saveDigestSchedule(ctx, msg, sched)
}

func (b *Bot) handleScheduleDayGroup(ctx context.Context, msg *tgbotapi.Message, dayTarget string, args []string) {
	if len(args) < 3 {
		b.reply(msg, "Usage: <code>/schedule weekdays times &lt;HH:00,...&gt;</code> | <code>/schedule weekdays hourly &lt;HH:00-HH:00&gt;</code>")

		return
	}

	mode := strings.ToLower(args[1])
	value := strings.Join(args[2:], " ")

	sched := b.loadDigestSchedule(ctx)

	var day *schedule.DaySchedule
	if dayTarget == SubCmdWeekdays {
		day = &sched.Weekdays
	} else {
		day = &sched.Weekends
	}

	switch mode {
	case SubCmdTimes:
		b.handleScheduleTimes(ctx, msg, day, value, &sched)
	case SubCmdHourly:
		b.handleScheduleHourly(ctx, msg, day, value, &sched)
	default:
		b.reply(msg, "❌ Unknown schedule mode. Use <code>times</code> or <code>hourly</code>.")
	}
}

func (b *Bot) handleScheduleClear(ctx context.Context, msg *tgbotapi.Message) {
	if err := b.database.DeleteSettingWithHistory(ctx, schedule.SettingDigestSchedule, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error clearing schedule: %s", html.EscapeString(err.Error())))

		return
	}

	if err := b.database.DeleteSettingWithHistory(ctx, schedule.SettingDigestScheduleAnchor, msg.From.ID); err != nil {
		b.logger.Debug().Err(err).Msg("failed to clear digest_schedule_anchor")
	}

	b.reply(msg, "✅ Digest schedule cleared. Configure a new schedule with <code>/schedule set</code>.")
}

func (b *Bot) handleSchedulePreview(ctx context.Context, msg *tgbotapi.Message, args []string) {
	count, err := parseSchedulePreviewCount(args)
	if err != nil {
		b.reply(msg, "Usage: <code>/schedule preview [count]</code>")

		return
	}

	message, err := b.formatSchedulePreview(ctx, count)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error computing schedule preview: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, message)
}

func parseSchedulePreviewCount(args []string) (int, error) {
	if len(args) <= 1 {
		return schedulePreviewDefault, nil
	}

	parsed, err := strconv.Atoi(args[1])
	if err != nil || parsed <= 0 {
		return 0, errInvalidPreviewCount
	}

	if parsed > schedulePreviewMax {
		parsed = schedulePreviewMax
	}

	return parsed, nil
}

func (b *Bot) formatSchedulePreview(ctx context.Context, count int) (string, error) {
	sched := b.loadDigestSchedule(ctx)
	if sched.IsEmpty() {
		return "ℹ️ No digest schedule configured.", nil
	}

	if err := sched.Validate(); err != nil {
		return fmt.Sprintf(errInvalidScheduleFmt, html.EscapeString(err.Error())), nil
	}

	loc, err := sched.Location()
	if err != nil {
		return fmt.Sprintf("❌ Invalid timezone: %s", html.EscapeString(err.Error())), nil
	}

	times, err := sched.NextTimes(time.Now(), count)
	if err != nil {
		return "", fmt.Errorf("compute schedule preview: %w", err)
	}

	if len(times) == 0 {
		return "ℹ️ No upcoming scheduled times found.", nil
	}

	var sb strings.Builder
	sb.WriteString("🗓️ <b>Next scheduled digests</b>\n")
	sb.WriteString(fmt.Sprintf(scheduleTimezoneLineFmt, html.EscapeString(loc.String())))

	for _, t := range times {
		sb.WriteString(fmt.Sprintf("• %s\n", t.In(loc).Format("Mon 2006-01-02 15:04")))
	}

	return sb.String(), nil
}

func (b *Bot) handleScheduleTimes(ctx context.Context, msg *tgbotapi.Message, day *schedule.DaySchedule, value string, sched *schedule.Schedule) {
	if strings.EqualFold(value, SubCmdClear) || strings.EqualFold(value, ToggleOff) {
		day.Times = nil

		b.saveDigestSchedule(ctx, msg, *sched)

		return
	}

	times, err := parseScheduleTimes(value)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Invalid time: %s", html.EscapeString(err.Error())))
		return
	}

	if len(times) == 0 {
		b.reply(msg, "❌ Provide a list of times, e.g. <code>09:00,13:00,18:00</code>.")

		return
	}

	day.Times = times

	b.saveDigestSchedule(ctx, msg, *sched)
}

func (b *Bot) handleScheduleHourly(ctx context.Context, msg *tgbotapi.Message, day *schedule.DaySchedule, value string, sched *schedule.Schedule) {
	if strings.EqualFold(value, SubCmdClear) || strings.EqualFold(value, ToggleOff) {
		day.Hourly = nil

		b.saveDigestSchedule(ctx, msg, *sched)

		return
	}

	start, end, err := parseHourlyRange(value)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Invalid hourly range: %s", html.EscapeString(err.Error())))

		return
	}

	day.Hourly = &schedule.HourlyRange{Start: start, End: end}

	b.saveDigestSchedule(ctx, msg, *sched)
}

func (b *Bot) loadDigestSchedule(ctx context.Context) schedule.Schedule {
	var sched schedule.Schedule
	if err := b.database.GetSetting(ctx, schedule.SettingDigestSchedule, &sched); err != nil {
		b.logger.Debug().Err(err).Msg("could not get digest_schedule from DB")
	}

	return sched
}

func (b *Bot) saveDigestSchedule(ctx context.Context, msg *tgbotapi.Message, sched schedule.Schedule) {
	sched.Timezone = schedule.NormalizeTimezone(sched.Timezone)

	if err := sched.Validate(); err != nil {
		b.reply(msg, fmt.Sprintf(errInvalidScheduleFmt, html.EscapeString(err.Error())))
		return
	}

	if err := b.database.SaveSettingWithHistory(ctx, schedule.SettingDigestSchedule, sched, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving schedule: %s", html.EscapeString(err.Error())))
		return
	}

	if err := b.database.SaveSettingWithHistory(ctx, schedule.SettingDigestScheduleAnchor, time.Now().UTC(), msg.From.ID); err != nil {
		b.logger.Debug().Err(err).Msg("failed to save digest_schedule_anchor")
	}

	b.reply(msg, "✅ Digest schedule updated.")
}

func (b *Bot) formatDigestSchedule(ctx context.Context) string {
	sched := b.loadDigestSchedule(ctx)

	timezone := sched.Timezone
	if strings.TrimSpace(timezone) == "" {
		timezone = "UTC"
	}

	if sched.IsEmpty() {
		return fmt.Sprintf("ℹ️ No digest schedule configured. Use <code>/schedule set</code> to configure. (Timezone: <code>%s</code>)", html.EscapeString(timezone))
	}

	var sb strings.Builder
	sb.WriteString("🗓️ <b>Digest Schedule</b>\n")
	sb.WriteString(fmt.Sprintf(scheduleTimezoneLineFmt, html.EscapeString(timezone)))
	sb.WriteString(fmt.Sprintf("• Weekdays: %s\n", html.EscapeString(formatScheduleDay(sched.Weekdays))))
	sb.WriteString(fmt.Sprintf("• Weekends: %s", html.EscapeString(formatScheduleDay(sched.Weekends))))

	return sb.String()
}

func formatScheduleDay(day schedule.DaySchedule) string {
	var parts []string

	if len(day.Times) > 0 {
		parts = append(parts, "times "+strings.Join(day.Times, ", "))
	}

	if day.Hourly != nil {
		parts = append(parts, fmt.Sprintf("hourly %s-%s", day.Hourly.Start, day.Hourly.End))
	}

	if len(parts) == 0 {
		return "none"
	}

	return strings.Join(parts, "; ")
}

func parseScheduleTimes(value string) ([]string, error) {
	var parts []string

	if strings.Contains(value, ",") {
		rawParts := strings.Split(value, ",")
		for _, part := range rawParts {
			part = strings.TrimSpace(part)
			if part != "" {
				parts = append(parts, part)
			}
		}
	} else {
		parts = strings.Fields(value)
	}

	if len(parts) == 0 {
		return nil, nil
	}

	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		normalizedTime, err := schedule.NormalizeTimeHM(part)
		if err != nil {
			return nil, fmt.Errorf("invalid time %q: %w", part, err)
		}

		normalized = append(normalized, normalizedTime)
	}

	return normalized, nil
}

func parseHourlyRange(value string) (string, string, error) {
	const (
		expectedParts         = 2
		errInvalidHourlyRange = "invalid hourly range: %w"
	)

	rangeParts := strings.SplitN(value, "-", expectedParts)
	if len(rangeParts) != expectedParts {
		return "", "", fmt.Errorf(errInvalidHourlyRange, schedule.ErrTimeFormat)
	}

	start := strings.TrimSpace(rangeParts[0])

	end := strings.TrimSpace(rangeParts[1])
	if start == "" || end == "" {
		return "", "", fmt.Errorf(errInvalidHourlyRange, schedule.ErrTimeFormat)
	}

	startNormalized, err := schedule.NormalizeTimeHM(start)
	if err != nil {
		return "", "", fmt.Errorf("invalid start time: %w", err)
	}

	endNormalized, err := schedule.NormalizeTimeHM(end)
	if err != nil {
		return "", "", fmt.Errorf("invalid end time: %w", err)
	}

	return startNormalized, endNormalized, nil
}

func (b *Bot) handleLanguage(ctx context.Context, msg *tgbotapi.Message) {
	args := msg.CommandArguments()

	if args == "" {
		b.reply(msg, "Usage: <code>/language &lt;lang_code&gt;</code> (e.g. <code>en</code>, <code>ru</code>, <code>de</code>)")

		return
	}

	if err := b.database.SaveSettingWithHistory(ctx, "digest_language", args, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving digest language: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Digest language updated to <code>%s</code>.", html.EscapeString(args)))
}

func (b *Bot) handleMinLength(ctx context.Context, msg *tgbotapi.Message) {
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

	if err := b.database.SaveSettingWithHistory(ctx, "filters_min_length", val, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving min length: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Minimum message length updated to <code>%d</code>.", val))
}

func (b *Bot) handleMaxLinks(ctx context.Context, msg *tgbotapi.Message) {
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

	if err := b.database.SaveSettingWithHistory(ctx, "max_links_per_message", val, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving max links: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Max links per message updated to <code>%d</code>.", val))
}

func (b *Bot) handleLinkCache(ctx context.Context, msg *tgbotapi.Message) {
	args := msg.CommandArguments()

	if args == "" {
		b.reply(msg, "Usage: <code>/link_cache &lt;duration&gt;</code> (e.g. <code>12h</code>, <code>24h</code>, <code>7d</code>)")

		return
	}

	durationStr := args

	if strings.HasSuffix(durationStr, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(durationStr, "d"))
		if err == nil {
			durationStr = fmt.Sprintf("%dh", days*HoursPerDay)
		}
	}

	_, err := time.ParseDuration(durationStr)
	if err != nil {
		b.reply(msg, "❌ Invalid duration format. Use something like <code>12h</code>, <code>24h</code>.")

		return
	}

	if err := b.database.SaveSettingWithHistory(ctx, "link_cache_ttl", args, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving link cache TTL: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Link cache TTL updated to <code>%s</code>.", html.EscapeString(args)))
}

func (b *Bot) handleAdsKeywords(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	keywords, err := b.getAdsKeywords(ctx)
	if err != nil {
		b.reply(msg, ErrFetchingAdsKeywords)

		return
	}

	if len(args) == 0 {
		b.reply(msg, fmt.Sprintf("📋 <b>Ads Keywords:</b>\n<code>%s</code>\n\nUsage: <code>/adskeywords add &lt;word&gt;</code> or <code>/adskeywords remove &lt;word&gt;</code> or <code>/adskeywords clear</code>", html.EscapeString(strings.Join(keywords, ", "))))

		return
	}

	newKeywords, ok := b.processAdsKeywordAction(msg, args, keywords)
	if !ok {
		return
	}

	if err := b.database.SaveSettingWithHistory(ctx, SettingFiltersAdsKeywords, newKeywords, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving ads keywords: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Ads keywords updated. Total: <code>%d</code>", len(newKeywords)))
}

func (b *Bot) getAdsKeywords(ctx context.Context) ([]string, error) {
	var keywords []string

	if err := b.database.GetSetting(ctx, SettingFiltersAdsKeywords, &keywords); err != nil {
		return nil, fmt.Errorf("getting ads keywords setting: %w", err)
	}

	if len(keywords) == 0 {
		keywords = []string{"#ad", "sponsored", "promo", "подпишись", "купи", "зарабатывай", "выигрывай"}
	}

	return keywords, nil
}

func (b *Bot) processAdsKeywordAction(msg *tgbotapi.Message, args []string, keywords []string) ([]string, bool) {
	switch args[0] {
	case CmdAdd:
		return b.addAdsKeyword(msg, args, keywords)
	case CmdRemove:
		return b.removeAdsKeyword(msg, args, keywords)
	case SubCmdClear:
		return []string{}, true
	default:
		b.reply(msg, "❓ Unknown command. Use <code>add</code>, <code>remove</code>, <code>clear</code> or no arguments to list.")

		return nil, false
	}
}

func (b *Bot) addAdsKeyword(msg *tgbotapi.Message, args []string, keywords []string) ([]string, bool) {
	if len(args) < 2 {
		b.reply(msg, "Usage: <code>/adskeywords add &lt;word&gt;</code>")

		return nil, false
	}

	word := strings.ToLower(args[1])

	for _, k := range keywords {
		if k == word {
			b.reply(msg, errKeywordAlreadyExists)

			return nil, false
		}
	}

	return append(keywords, word), true
}

func (b *Bot) removeAdsKeyword(msg *tgbotapi.Message, args []string, keywords []string) ([]string, bool) {
	if len(args) < 2 {
		b.reply(msg, "Usage: <code>/adskeywords remove &lt;word&gt;</code>")

		return nil, false
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
		b.reply(msg, errKeywordNotFound)

		return nil, false
	}

	return newKeywords, true
}

func (b *Bot) handleListChannels(ctx context.Context, msg *tgbotapi.Message) {
	channels, err := b.database.GetActiveChannels(ctx)
	if err != nil {
		b.reply(msg, fmt.Sprintf(ErrFetchingChannelsFmt, html.EscapeString(err.Error())))

		return
	}

	if len(channels) == 0 {
		b.reply(msg, "No active channels tracked.")

		return
	}

	var sb strings.Builder

	sb.WriteString("📋 <b>Active Tracked Channels:</b>\n\n")

	for _, ch := range channels {
		formatChannelEntry(&sb, ch)
	}

	sb.WriteString("\n💡 <i>Use <code>/channel weight</code> or <code>/channel relevance</code> to manage channel quality controls.</i>")
	b.reply(msg, sb.String())
}

func formatChannelEntry(sb *strings.Builder, ch db.Channel) {
	identifier := fmt.Sprintf("@%s", html.EscapeString(ch.Username))
	if ch.Username == "" {
		identifier = fmt.Sprintf("ID: <code>%d</code>", ch.TGPeerID)
	}

	title := ch.Title
	if title == "" {
		title = "Pending..."
	}

	fmt.Fprintf(sb, "• %s (%s)\n", html.EscapeString(title), identifier)

	weightStr := fmt.Sprintf("%.1fx", ch.ImportanceWeight)
	if ch.WeightOverride {
		weightStr += " (manual)"
	}

	fmt.Fprintf(sb, "  Weight: <code>%s</code>\n", weightStr)

	relevanceStr := WeightOverrideManual

	if ch.AutoRelevanceEnabled {
		if ch.RelevanceThresholdDelta != 0 {
			relevanceStr = fmt.Sprintf("%s (%+.2f)", SubCmdAuto, ch.RelevanceThresholdDelta)
		} else {
			relevanceStr = SubCmdAuto
		}
	}

	fmt.Fprintf(sb, "  Relevance: <code>%s</code>\n", relevanceStr)

	if ch.Context != "" {
		fmt.Fprintf(sb, "  <i>Context: %s</i>\n", html.EscapeString(ch.Context))
	}

	if ch.Description != "" {
		fmt.Fprintf(sb, "  <i>Description: %s</i>\n", html.EscapeString(ch.Description))
	}

	formatChannelMetadata(sb, ch)
}

func formatChannelMetadata(sb *strings.Builder, ch db.Channel) {
	if ch.Category == "" && ch.Tone == "" && ch.UpdateFreq == "" {
		return
	}

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

	fmt.Fprintf(sb, "  <i>Metadata: %s</i>\n", html.EscapeString(strings.TrimSpace(meta)))
}

func (b *Bot) handleChannelStats(ctx context.Context, msg *tgbotapi.Message) {
	stats, err := b.database.GetChannelStats(ctx)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching channel stats: %s", html.EscapeString(err.Error())))

		return
	}

	channels, err := b.database.GetActiveChannels(ctx)
	if err != nil {
		b.reply(msg, fmt.Sprintf(ErrFetchingChannelsFmt, html.EscapeString(err.Error())))

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

func (b *Bot) handleRatings(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) > 0 && strings.EqualFold(args[0], "stats") {
		b.handleRatingsStats(ctx, msg, args[1:])
		return
	}

	b.handleRatingsSummary(ctx, msg, args)
}

func (b *Bot) handleRatingsSummary(ctx context.Context, msg *tgbotapi.Message, args []string) {
	days, limit := parseRatingsDaysLimit(args)
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

	totalGood, totalBad, totalIrrelevant, totalAll := computeRatingTotals(summaries)

	if limit > len(summaries) {
		limit = len(summaries)
	}

	b.reply(msg, formatRatingsSummaryOutput(days, limit, summaries, totalGood, totalBad, totalIrrelevant, totalAll))
}

func formatRatingsSummaryOutput(days, limit int, summaries []db.RatingSummary, good, bad, irrelevant, total int) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("⭐ <b>Item Ratings (last %d days)</b>\n\n", days))
	sb.WriteString(fmt.Sprintf("Total: <code>%d</code> (good %d | bad %d | irrelevant %d)\n\n", total, good, bad, irrelevant))

	for i := 0; i < limit; i++ {
		s := summaries[i]
		name := formatRatingsChannelName(s.ChannelID, s.Username, s.Title)

		reliability := DefaultReliabilityZero
		if s.TotalCount > 0 {
			reliability = float64(s.GoodCount) / float64(s.TotalCount)
		}

		sb.WriteString(fmt.Sprintf("• <b>%s</b>: <code>%d</code> (g %d | b %d | i %d) rel <code>%.2f</code>\n",
			html.EscapeString(name), s.TotalCount, s.GoodCount, s.BadCount, s.IrrelevantCount, reliability))
	}

	return sb.String()
}

func (b *Bot) handleRatingsStats(ctx context.Context, msg *tgbotapi.Message, args []string) {
	limit := DefaultRatingsLimit

	if len(args) > 0 {
		if v, err := strconv.Atoi(args[0]); err == nil && v > 0 {
			limit = v
		} else {
			b.reply(msg, "Usage: <code>/ratings stats [limit]</code>")

			return
		}
	}

	entries, err := b.database.GetLatestChannelRatingStats(ctx, limit)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching rating stats: %s", html.EscapeString(err.Error())))

		return
	}

	if len(entries) == 0 {
		b.reply(msg, "No aggregated rating stats yet. The weekly job updates these automatically.")

		return
	}

	global, err := b.database.GetLatestGlobalRatingStats(ctx)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching global rating stats: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, formatRatingsStatsOutput(entries, global))
}

func formatRatingsStatsOutput(entries []db.RatingStatsSummary, global *db.GlobalRatingStats) string {
	periodStart := entries[0].PeriodStart
	periodEnd := entries[0].PeriodEnd

	if global != nil {
		periodStart = global.PeriodStart
		periodEnd = global.PeriodEnd
	}

	var sb strings.Builder

	sb.WriteString("⭐ <b>Weighted Rating Stats</b>\n\n")
	sb.WriteString(fmt.Sprintf("Window: <code>%s</code> - <code>%s</code>\n",
		periodStart.Format(DateFormatYMD),
		periodEnd.Format(DateFormatYMD),
	))

	formatGlobalStats(&sb, global)

	for _, entry := range entries {
		name := formatRatingsChannelName(entry.ChannelID, entry.Username, entry.Title)

		reliability := DefaultReliabilityZero
		if entry.WeightedTotal > 0 {
			reliability = entry.WeightedGood / entry.WeightedTotal
		}

		sb.WriteString(fmt.Sprintf("• <b>%s</b>: <code>%.2f</code> (w %.1f | n %d)\n",
			html.EscapeString(name),
			reliability,
			entry.WeightedTotal,
			entry.RatingCount,
		))
	}

	return sb.String()
}

func formatGlobalStats(sb *strings.Builder, global *db.GlobalRatingStats) {
	if global == nil {
		sb.WriteString("Global: <code>n/a</code>\n\n")

		return
	}

	reliability := DefaultReliabilityZero
	if global.WeightedTotal > 0 {
		reliability = global.WeightedGood / global.WeightedTotal
	}

	fmt.Fprintf(sb, "Global: <code>%.2f</code> (w %.1f | n %d)\n\n",
		reliability,
		global.WeightedTotal,
		global.RatingCount,
	)
}

func (b *Bot) handleScores(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) > 0 && strings.EqualFold(args[0], "debug") {
		b.handleScoresDebug(ctx, msg, args[1:])
		return
	}

	hours, limit := parseScoresArgs(args)

	if hours <= 0 || limit <= 0 {
		b.reply(msg, "Usage: <code>/scores [hours] [limit]</code>")

		return
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour)

	importanceThreshold := b.cfg.ImportanceThreshold
	if err := b.database.GetSetting(ctx, SettingImportanceThreshold, &importanceThreshold); err != nil {
		b.logger.Debug().Err(err).Msg(MsgCouldNotGetImportanceThreshold)
	}

	stats, err := b.database.GetImportanceStats(ctx, since, importanceThreshold)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching scores: %s", html.EscapeString(err.Error())))

		return
	}

	if stats.Total == 0 {
		b.reply(msg, fmt.Sprintf("No ready items in the last %d hours.", hours))

		return
	}

	items, err := b.database.GetTopItemScores(ctx, since, limit)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching items: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, formatScoresOutput(hours, importanceThreshold, &stats, items))
}

func (b *Bot) handleScoresDebug(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) > 0 && strings.EqualFold(args[0], "reasons") {
		b.handleScoresDebugReasons(ctx, msg, args[1:])
		return
	}

	hours, valid := parseScoresDebugArgs(args)
	if !valid {
		b.reply(msg, MsgScoresDebugUsage)

		return
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour)

	debugStats, err := b.database.GetScoreDebugStats(ctx, since)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching score stats: %s", html.EscapeString(err.Error())))

		return
	}

	itemStats, err := b.database.GetItemStatusStats(ctx, since)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching item stats: %s", html.EscapeString(err.Error())))

		return
	}

	if debugStats.RawTotal == 0 && itemStats.Total == 0 {
		b.reply(msg, fmt.Sprintf("No messages in the last %d hours.", hours))

		return
	}

	b.reply(msg, formatScoresDebugOutput(hours, debugStats, itemStats))
}

func (b *Bot) handleScoresDebugReasons(ctx context.Context, msg *tgbotapi.Message, args []string) {
	hours, valid := parseScoresDebugArgs(args)
	if !valid {
		b.reply(msg, MsgScoresDebugReasonsUsage)

		return
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour)

	reasons, err := b.database.GetDropReasonStats(ctx, since, DefaultScoresLimit)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching drop reasons: %s", html.EscapeString(err.Error())))

		return
	}

	if len(reasons) == 0 {
		b.reply(msg, fmt.Sprintf("No drop reasons logged in the last %d hours.", hours))

		return
	}

	var sb strings.Builder

	total := 0

	sb.WriteString(fmt.Sprintf("📊 <b>Drop Reasons (last %d hours)</b>\n\n", hours))

	for _, entry := range reasons {
		sb.WriteString(fmt.Sprintf(statsItemFormat, html.EscapeString(entry.Reason), entry.Count))
		total += entry.Count
	}

	sb.WriteString(fmt.Sprintf("\nTotal logged: <code>%d</code>\n", total))

	b.reply(msg, sb.String())
}

func parseScoresDebugArgs(args []string) (int, bool) {
	if len(args) == 0 {
		return DefaultScoresHours, true
	}

	if len(args) > 1 {
		return 0, false
	}

	v, err := strconv.Atoi(args[0])
	if err != nil || v <= 0 {
		return 0, false
	}

	return v, true
}

func formatScoresDebugOutput(hours int, debugStats db.ScoreDebugStats, itemStats db.ItemStatusStats) string {
	rawUnprocessed := debugStats.RawTotal - debugStats.RawProcessed
	if rawUnprocessed < 0 {
		rawUnprocessed = 0
	}

	droppedBeforeItem := debugStats.RawProcessed - debugStats.ItemsTotal
	if droppedBeforeItem < 0 {
		droppedBeforeItem = 0
	}

	gateTotal := debugStats.GateRelevant + debugStats.GateIrrelevant

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("📊 <b>Item Status (last %d hours)</b>\n\n", hours))
	sb.WriteString(fmt.Sprintf("Raw messages: <code>%d</code>\n", debugStats.RawTotal))
	sb.WriteString(fmt.Sprintf("Processed: <code>%d</code>\n", debugStats.RawProcessed))
	sb.WriteString(fmt.Sprintf("Unprocessed: <code>%d</code>\n", rawUnprocessed))
	sb.WriteString(fmt.Sprintf("Dropped before item: <code>%d</code>\n", droppedBeforeItem))
	sb.WriteString(fmt.Sprintf("Items created: <code>%d</code>\n\n", itemStats.Total))
	sb.WriteString(fmt.Sprintf("Ready (pending): <code>%d</code>\n", itemStats.ReadyPending))
	sb.WriteString(fmt.Sprintf("Ready (digested): <code>%d</code>\n", itemStats.ReadyDigested))
	sb.WriteString(fmt.Sprintf("Rejected: <code>%d</code>\n", itemStats.Rejected))
	sb.WriteString(fmt.Sprintf("Error: <code>%d</code>\n", itemStats.Error))
	sb.WriteString(fmt.Sprintf("\nRelevance gate decisions: <code>%d</code> (rel %d | irrel %d)\n", gateTotal, debugStats.GateRelevant, debugStats.GateIrrelevant))

	return sb.String()
}

func parseScoresArgs(args []string) (hours, limit int) {
	hours = DefaultScoresHours
	limit = DefaultScoresLimit

	if len(args) > 0 {
		if v, err := strconv.Atoi(args[0]); err == nil && v > 0 {
			hours = v
		}
	}

	if len(args) > 1 {
		if v, err := strconv.Atoi(args[1]); err == nil && v > 0 {
			limit = v
		}
	}

	return hours, limit
}

func formatScoresOutput(hours int, threshold float32, stats *db.ImportanceStats, items []db.ItemScore) string {
	var sb strings.Builder

	belowCount := stats.Total - stats.AboveThreshold

	sb.WriteString(fmt.Sprintf("📊 <b>Item Importance Scores</b> (last %d hours)\n\n", hours))
	sb.WriteString(fmt.Sprintf("Threshold: <code>%.2f</code>\n", threshold))
	sb.WriteString(fmt.Sprintf("Ready items: <code>%d</code> | >= threshold: <code>%d</code> | below: <code>%d</code>\n", stats.Total, stats.AboveThreshold, belowCount))
	sb.WriteString(fmt.Sprintf("Percentiles: p50 <code>%.2f</code> | p75 <code>%.2f</code> | p90 <code>%.2f</code> | p95 <code>%.2f</code>\n", stats.P50, stats.P75, stats.P90, stats.P95))
	sb.WriteString(fmt.Sprintf("Range: <code>%.2f</code> - <code>%.2f</code>\n\n", stats.Min, stats.Max))

	if len(items) == 0 {
		sb.WriteString("No ready items to display.\n")

		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("Top %d items:\n", len(items)))

	for _, item := range items {
		formatScoreItem(&sb, item, threshold)
	}

	return sb.String()
}

func formatScoreItem(sb *strings.Builder, item db.ItemScore, threshold float32) {
	name := item.Username
	if name != "" {
		name = "@" + name
	} else if item.Title != "" {
		name = item.Title
	} else {
		name = annotateUnknown
	}

	summary := item.Summary
	if summary == "" {
		summary = "(no summary)"
	} else {
		summary = htmlutils.SanitizeHTML(summary)
	}

	marker := ""
	if item.Importance < float64(threshold) {
		marker = "⬇ "
	}

	fmt.Fprintf(sb, "• %s<code>%.2f</code> (rel %.2f) %s - %s\n",
		marker,
		item.Importance,
		item.Relevance,
		html.EscapeString(name),
		summary,
	)
}

// parseRatingsDaysLimit parses days and limit from args for ratings commands.
func parseRatingsDaysLimit(args []string) (days, limit int) {
	days = DefaultRatingsDays
	limit = DefaultRatingsLimit

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

	return days, limit
}

// computeRatingTotals calculates aggregate totals from rating summaries.
func computeRatingTotals(summaries []db.RatingSummary) (good, bad, irrelevant, total int) {
	for _, s := range summaries {
		good += s.GoodCount
		bad += s.BadCount
		irrelevant += s.IrrelevantCount
		total += s.TotalCount
	}

	return good, bad, irrelevant, total
}

// formatRatingsChannelName formats a channel name for display in ratings.
func formatRatingsChannelName(channelID, username, title string) string {
	if username != "" {
		return "@" + username
	}

	if title != "" {
		return title
	}

	return channelID
}

func (b *Bot) handlePrompt(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 {
		b.replyPromptUsage(msg)

		return
	}

	switch strings.ToLower(args[0]) {
	case "list":
		b.handlePromptList(ctx, msg)
	case "show":
		b.handlePromptShow(ctx, msg, args)
	case subCmdSet:
		b.handlePromptSet(ctx, msg, args)
	case "activate", "active":
		b.handlePromptActivate(ctx, msg, args)
	default:
		b.replyPromptUsage(msg)
	}
}

func (b *Bot) replyPromptUsage(msg *tgbotapi.Message) {
	b.reply(msg, "Usage:\n"+
		"<code>/prompt list</code>\n"+
		"<code>/prompt show &lt;summarize|narrative|cluster_summary|cluster_topic|relevance_gate&gt; [version]</code>\n"+
		"<code>/prompt set &lt;base&gt; &lt;version&gt; &lt;text...&gt;</code>\n"+
		"<code>/prompt activate &lt;base&gt; &lt;version&gt;</code>")
}

func (b *Bot) isValidPromptBase(v string) bool {
	for _, baseName := range promptBases {
		if baseName == v {
			return true
		}
	}

	return false
}

func (b *Bot) handlePromptList(ctx context.Context, msg *tgbotapi.Message) {
	var sb strings.Builder

	sb.WriteString("🧩 <b>Prompt Templates</b>\n\n")

	for _, baseName := range promptBases {
		activeKey := fmt.Sprintf(PromptActiveKeyFmt, baseName)
		active := "v1"
		_ = b.database.GetSetting(ctx, activeKey, &active) //nolint:errcheck // best-effort read
		sb.WriteString(fmt.Sprintf("• <b>%s</b> active: <code>%s</code>\n", html.EscapeString(baseName), html.EscapeString(active)))
	}

	b.reply(msg, sb.String())
}

func (b *Bot) handlePromptShow(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) < 2 {
		b.reply(msg, "Usage: <code>/prompt show &lt;base&gt; [version]</code>")

		return
	}

	baseName := strings.ToLower(args[1])
	if !b.isValidPromptBase(baseName) {
		b.reply(msg, fmt.Sprintf(ErrUnknownBaseFmt, html.EscapeString(strings.Join(promptBases, ", "))))

		return
	}

	version := "v1"
	if len(args) > 2 {
		version = args[2]
	} else {
		activeKey := fmt.Sprintf(PromptActiveKeyFmt, baseName)
		_ = b.database.GetSetting(ctx, activeKey, &version) //nolint:errcheck // best-effort read

		if version == "" {
			version = "v1"
		}
	}

	promptKey := fmt.Sprintf(PromptKeyFmt, baseName, version)

	var prompt string

	_ = b.database.GetSetting(ctx, promptKey, &prompt) //nolint:errcheck // best-effort read
	if prompt == "" {
		b.reply(msg, fmt.Sprintf("No override found for <code>%s</code> (version <code>%s</code>). Using built-in default.", html.EscapeString(baseName), html.EscapeString(version)))

		return
	}

	escaped := html.EscapeString(prompt)

	b.reply(msg, fmt.Sprintf("Prompt <b>%s</b> (<code>%s</code>):\n<pre>%s</pre>", html.EscapeString(baseName), html.EscapeString(version), escaped))
}

func (b *Bot) handlePromptSet(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) < 4 {
		b.reply(msg, "Usage: <code>/prompt set &lt;base&gt; &lt;version&gt; &lt;text...&gt;</code>")

		return
	}

	baseName := strings.ToLower(args[1])
	if !b.isValidPromptBase(baseName) {
		b.reply(msg, fmt.Sprintf(ErrUnknownBaseFmt, html.EscapeString(strings.Join(promptBases, ", "))))

		return
	}

	version := args[2]
	text := strings.Join(args[3:], " ")

	key := fmt.Sprintf(PromptKeyFmt, baseName, version)
	if err := b.database.SaveSettingWithHistory(ctx, key, text, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving prompt: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Prompt <b>%s</b> saved as <code>%s</code>.", html.EscapeString(baseName), html.EscapeString(version)))
}

func (b *Bot) handlePromptActivate(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) < 3 {
		b.reply(msg, "Usage: <code>/prompt activate &lt;base&gt; &lt;version&gt;</code>")

		return
	}

	baseName := strings.ToLower(args[1])
	if !b.isValidPromptBase(baseName) {
		b.reply(msg, fmt.Sprintf(ErrUnknownBaseFmt, html.EscapeString(strings.Join(promptBases, ", "))))

		return
	}

	version := args[2]

	key := fmt.Sprintf(PromptActiveKeyFmt, baseName)
	if err := b.database.SaveSettingWithHistory(ctx, key, version, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving active version: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Active prompt for <b>%s</b> set to <code>%s</code>.", html.EscapeString(baseName), html.EscapeString(version)))
}

func (b *Bot) handleChannelWeight(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

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
	if len(args) == 1 && (identifier == SubCmdAuto || isNumericWeight(identifier)) {
		b.reply(msg, "Missing channel identifier.\nUsage: <code>/channel weight @username</code> or <code>/channel weight @username 1.5</code>")

		return
	}

	// Just identifier - show current weight
	if len(args) == 1 {
		b.showChannelWeight(ctx, msg, identifier)

		return
	}

	// Set weight or enable auto
	b.setChannelWeight(ctx, msg, identifier, args[1:])
}

// showChannelWeight displays the current weight for a channel.
func (b *Bot) showChannelWeight(ctx context.Context, msg *tgbotapi.Message, identifier string) {
	weight, err := b.database.GetChannelWeight(ctx, identifier)
	if err != nil {
		if strings.Contains(err.Error(), ErrNoRows) {
			b.reply(msg, fmt.Sprintf("Channel <code>@%s</code> not found.", html.EscapeString(identifier)))
		} else {
			b.reply(msg, fmt.Sprintf(ErrGenericFmt, html.EscapeString(err.Error())))
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
}

// setChannelWeight sets the weight for a channel (manual or auto mode).
func (b *Bot) setChannelWeight(ctx context.Context, msg *tgbotapi.Message, identifier string, args []string) {
	weightArg := args[0]

	if weightArg == SubCmdAuto {
		b.enableAutoWeight(ctx, msg, identifier)

		return
	}

	weight, err := strconv.ParseFloat(weightArg, 32)
	if err != nil || weight < 0.1 || weight > 2.0 {
		b.reply(msg, "Invalid weight. Use a number between 0.1 and 2.0, or 'auto' to reset to default.")

		return
	}

	reason := ""
	if len(args) > 1 {
		reason = strings.Join(args[1:], " ")
	}

	// Manual weight: autoEnabled=false, override=true
	result, err := b.database.UpdateChannelWeight(ctx, identifier, float32(weight), false, true, reason, msg.From.ID)
	if err != nil {
		b.replyChannelUpdateError(msg, err, identifier)

		return
	}

	chanDisplay := formatChannelDisplay(result.Username, result.Title, identifier)
	reply := fmt.Sprintf("Weight for %s set to <code>%.2f</code>", chanDisplay, weight)

	if reason != "" {
		reply += fmt.Sprintf("\nReason: <i>%s</i>", html.EscapeString(reason))
	}

	b.reply(msg, reply)
}

// enableAutoWeight enables auto-weight calculation for a channel.
func (b *Bot) enableAutoWeight(ctx context.Context, msg *tgbotapi.Message, identifier string) {
	result, err := b.database.UpdateChannelWeight(ctx, identifier, 1.0, true, false, "", msg.From.ID)
	if err != nil {
		b.replyChannelUpdateError(msg, err, identifier)

		return
	}

	chanDisplay := formatChannelDisplay(result.Username, result.Title, identifier)
	b.reply(msg, fmt.Sprintf("Auto-weight enabled for %s. Weight reset to 1.0.", chanDisplay))
}

// replyChannelUpdateError sends an appropriate error message for channel update failures.
func (b *Bot) replyChannelUpdateError(msg *tgbotapi.Message, err error, identifier string) {
	if strings.Contains(err.Error(), ErrNoRows) {
		b.reply(msg, fmt.Sprintf(ErrChannelNotFoundFmt, html.EscapeString(identifier)))
	} else {
		b.reply(msg, fmt.Sprintf(ErrGenericFmt, html.EscapeString(err.Error())))
	}
}

func (b *Bot) handleChannelRelevance(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.replyChannelRelevanceUsage(msg)

		return
	}

	identifier := strings.TrimPrefix(args[0], "@")

	if isRelevanceKeyword(identifier) && len(args) == 1 {
		b.reply(msg, "Missing channel identifier.\nUsage: <code>/channel relevance @username</code>")

		return
	}

	channel, errMsg := b.lookupChannel(ctx, identifier)
	if errMsg != "" {
		b.reply(msg, errMsg)

		return
	}

	if len(args) == 1 {
		b.showChannelRelevanceStatus(ctx, msg, channel, identifier)

		return
	}

	b.dispatchRelevanceAction(ctx, msg, channel, identifier, args[1])
}

func isRelevanceKeyword(s string) bool {
	return s == SubCmdAuto || s == WeightOverrideManual || s == ToggleOff || s == "on"
}

func (b *Bot) lookupChannel(ctx context.Context, identifier string) (*db.Channel, string) {
	channels, err := b.database.GetActiveChannels(ctx)
	if err != nil {
		return nil, fmt.Sprintf(ErrFetchingChannelsFmt, html.EscapeString(err.Error()))
	}

	channel := findChannelByIdentifier(channels, identifier)
	if channel == nil {
		return nil, fmt.Sprintf(ErrChannelNotFoundFmt, html.EscapeString(identifier))
	}

	return channel, ""
}

func (b *Bot) dispatchRelevanceAction(ctx context.Context, msg *tgbotapi.Message, channel *db.Channel, identifier, action string) {
	switch strings.ToLower(action) {
	case "auto", "on", "enable":
		b.setChannelAutoRelevance(ctx, msg, channel, identifier, true)
	case WeightOverrideManual, ToggleOff, ToggleDisable:
		b.setChannelAutoRelevance(ctx, msg, channel, identifier, false)
	default:
		b.replyChannelRelevanceUsage(msg)
	}
}

// replyChannelRelevanceUsage sends the usage help for the channel relevance command.
func (b *Bot) replyChannelRelevanceUsage(msg *tgbotapi.Message) {
	b.reply(msg, "Usage:\n"+
		"<code>/channel relevance @username</code> - Show current auto relevance\n"+
		"<code>/channel relevance @username auto</code> - Enable auto relevance\n"+
		"<code>/channel relevance @username manual</code> - Disable auto relevance")
}

// showChannelRelevanceStatus displays the current relevance settings for a channel.
func (b *Bot) showChannelRelevanceStatus(ctx context.Context, msg *tgbotapi.Message, channel *db.Channel, identifier string) {
	globalThreshold := b.cfg.RelevanceThreshold
	_ = b.database.GetSetting(ctx, "SettingRelevanceThreshold", &globalThreshold) //nolint:errcheck // best-effort read

	baseThreshold, baseLabel := channel.RelevanceThreshold, CmdChannel
	if baseThreshold <= 0 {
		baseThreshold, baseLabel = globalThreshold, "global"
	}

	effective := baseThreshold
	if channel.AutoRelevanceEnabled {
		effective = clampFloat32(baseThreshold+channel.RelevanceThresholdDelta, 0, 1)
	}

	chanDisplay := formatChannelDisplay(channel.Username, channel.Title, identifier)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>Channel Relevance: %s</b>\n\n", chanDisplay))
	sb.WriteString(fmt.Sprintf("Base threshold: <code>%.2f</code> (%s)\n", baseThreshold, baseLabel))
	sb.WriteString(fmt.Sprintf("Auto relevance: <code>%t</code>\n", channel.AutoRelevanceEnabled))

	if channel.AutoRelevanceEnabled {
		sb.WriteString(fmt.Sprintf("Delta: <code>%+.2f</code>\n", channel.RelevanceThresholdDelta))
		sb.WriteString(fmt.Sprintf("Effective threshold: <code>%.2f</code>\n", effective))
	}

	b.reply(msg, sb.String())
}

// setChannelAutoRelevance enables or disables auto relevance for a channel.
func (b *Bot) setChannelAutoRelevance(ctx context.Context, msg *tgbotapi.Message, channel *db.Channel, identifier string, enable bool) {
	if err := b.database.UpdateChannelRelevanceDelta(ctx, channel.ID, 0, enable); err != nil {
		action := "enabling"
		if !enable {
			action = "disabling"
		}

		b.reply(msg, fmt.Sprintf("❌ Error %s auto relevance: %s", action, html.EscapeString(err.Error())))

		return
	}

	chanDisplay := formatChannelDisplay(channel.Username, channel.Title, identifier)
	action := "enabled"

	if !enable {
		action = "disabled"
	}

	b.reply(msg, fmt.Sprintf("Auto relevance %s for %s. Delta reset to 0.", action, chanDisplay))
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

func findChannelByIdentifier(channels []db.Channel, identifier string) *db.Channel {
	ident := strings.TrimPrefix(identifier, "@")
	if ident == "" {
		return nil
	}

	if idVal, err := strconv.ParseInt(ident, 10, 64); err == nil {
		for i := range channels {
			if channels[i].TGPeerID == idVal {
				return &channels[i]
			}
		}
	}

	for i := range channels {
		if strings.EqualFold(channels[i].Username, ident) {
			return &channels[i]
		}
	}

	return nil
}

func clampFloat32(val float32, minVal float32, maxVal float32) float32 {
	if val < minVal {
		return minVal
	}

	if val > maxVal {
		return maxVal
	}

	return val
}

func (b *Bot) handleFeedback(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) < 2 {
		b.reply(msg, "Usage: <code>/feedback &lt;item_id&gt; &lt;good|bad|irrelevant&gt; [comment]</code>")

		return
	}

	itemID := args[0]
	rating := strings.ToLower(args[1])

	if rating != RatingGood && rating != RatingBad && rating != RatingIrrelevant {
		b.reply(msg, fmt.Sprintf("❌ Invalid rating. Use <code>%s</code>, <code>%s</code>, or <code>%s</code>.", RatingGood, RatingBad, RatingIrrelevant))

		return
	}

	feedback := ""

	if len(args) > 2 {
		feedback = strings.Join(args[2:], " ")
	}

	if err := b.database.SaveItemRating(ctx, itemID, msg.From.ID, rating, feedback); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving feedback: %s", html.EscapeString(err.Error())))

		return
	}

	observability.ItemRatingsTotal.WithLabelValues(rating).Inc()
	b.reply(msg, fmt.Sprintf("✅ Feedback for item <code>%s</code> recorded as <b>%s</b>.", html.EscapeString(itemID), html.EscapeString(rating)))
}

func (b *Bot) handleChannelMetadata(ctx context.Context, msg *tgbotapi.Message) {
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
		rel, _ = strconv.ParseFloat(args[4], 32) //nolint:errcheck // best-effort read
	}

	if len(args) > 5 && args[5] != "-" {
		imp, _ = strconv.ParseFloat(args[5], 32) //nolint:errcheck // best-effort read
	}

	username := strings.TrimPrefix(identifier, "@")

	if err := b.database.UpdateChannelMetadata(ctx, username, category, tone, freq, float32(rel), float32(imp)); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error updating channel metadata: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Metadata updated for channel <code>%s</code>.", html.EscapeString(identifier)))
}

func (b *Bot) handleAddChannel(ctx context.Context, msg *tgbotapi.Message) {
	args := msg.CommandArguments()

	if args == "" {
		b.reply(msg, "Usage: <code>/add &lt;@username|ID|invite_link&gt;</code>")

		return
	}

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

func (b *Bot) handleRemoveChannel(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.reply(msg, "Usage: <code>/remove &lt;@username|ID&gt;</code>")

		return
	}

	identifier := args[0]

	if len(args) < 2 || args[1] != SubCmdConfirm {
		b.reply(msg, fmt.Sprintf("⚠️ Are you sure you want to stop tracking channel <code>%s</code>?\nUse <code>/remove %s confirm</code> to proceed.", html.EscapeString(identifier), html.EscapeString(identifier)))

		return
	}

	if err := b.database.DeactivateChannel(ctx, identifier); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error removing channel: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Channel <code>%s</code> removed.", html.EscapeString(identifier)))
}

func (b *Bot) handleFiltersAdd(ctx context.Context, msg *tgbotapi.Message, args []string) {
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
}

func (b *Bot) handleFiltersRemove(ctx context.Context, msg *tgbotapi.Message, args []string) {
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
}

func (b *Bot) handleFiltersAds(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) < 2 {
		b.reply(msg, "Usage: <code>/filters ads &lt;on|off&gt;</code>")

		return
	}

	enabled := args[1] == "on"

	if err := b.database.SaveSettingWithHistory(ctx, SettingFiltersAds, enabled, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving ads filter setting: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Ads filter turned <code>%s</code>.", strings.ToUpper(args[1])))
}

func (b *Bot) handleFiltersMode(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) < 2 {
		b.reply(msg, "Usage: <code>/filters mode &lt;mixed|allowlist|denylist&gt;</code>")

		return
	}

	mode := strings.ToLower(args[1])
	validModes := map[string]bool{"mixed": true, "allowlist": true, "denylist": true}

	if !validModes[mode] {
		b.reply(msg, "❌ Invalid mode. Use <code>mixed</code>, <code>allowlist</code> or <code>denylist</code>.")

		return
	}

	if err := b.database.SaveSettingWithHistory(ctx, "filters_mode", mode, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving filters mode: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Filters mode set to <code>%s</code>.", html.EscapeString(mode)))
}

func (b *Bot) handleFiltersList(ctx context.Context, msg *tgbotapi.Message) {
	filters, err := b.database.GetActiveFilters(ctx)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching filters: %s", html.EscapeString(err.Error())))

		return
	}

	var adsEnabled bool

	_ = b.database.GetSetting(ctx, SettingFiltersAds, &adsEnabled) //nolint:errcheck // best-effort read

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
}

func (b *Bot) handleFilters(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.handleFiltersList(ctx, msg)

		return
	}

	type filtersHandler func(context.Context, *tgbotapi.Message, []string)

	handlers := map[string]filtersHandler{
		CmdAdd:     b.handleFiltersAdd,
		CmdRemove:  b.handleFiltersRemove,
		SubCmdAds:  b.handleFiltersAds,
		SubCmdMode: b.handleFiltersMode,
	}

	if handler, ok := handlers[args[0]]; ok {
		handler(ctx, msg, args)
	} else {
		b.reply(msg, "❓ Unknown filters command. Use <code>add</code>, <code>remove</code>, <code>ads</code>, <code>mode</code> or no arguments to list.")
	}
}

func (b *Bot) handleTopics(ctx context.Context, msg *tgbotapi.Message) {
	b.handleToggleSetting(ctx, msg, "topics_enabled")
}

func (b *Bot) handleToggleSetting(ctx context.Context, msg *tgbotapi.Message, key string) {
	args := msg.CommandArguments()

	if args != "on" && args != ToggleOff {
		cmdName := strings.TrimSuffix(key, "_enabled")
		cmdName = strings.ReplaceAll(cmdName, "_", " ")
		b.reply(msg, fmt.Sprintf("Usage: <code>/%s &lt;on|off&gt;</code>", html.EscapeString(cmdName)))

		return
	}

	enabled := args == "on"

	var current bool

	_ = b.database.GetSetting(ctx, key, &current) //nolint:errcheck // best-effort read

	if err := b.database.SaveSettingWithHistory(ctx, key, enabled, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf(ErrSavingFmt, html.EscapeString(key), html.EscapeString(err.Error())))

		return
	}

	label := cases.Title(language.English).String(strings.ReplaceAll(key, "_", " "))
	status := StatusEnabled

	if !enabled {
		status = StatusDisabled
	}

	oldStatus := StatusEnabled
	if !current {
		oldStatus = StatusDisabled
	}

	b.reply(msg, fmt.Sprintf("✅ <b>%s</b>\nOld status: <code>%s</code>\nNew status: <code>%s</code>", html.EscapeString(label), oldStatus, status))
}

func (b *Bot) handleSetup(ctx context.Context, msg *tgbotapi.Message) {
	var targetID int64

	_ = b.database.GetSetting(ctx, SettingTargetChatID, &targetID) //nolint:errcheck // best-effort read

	channels, _ := b.database.GetActiveChannels(ctx) //nolint:errcheck // best-effort read

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
	sb.WriteString("• <code>/window 60m</code> - Set fallback digest interval\n")
	sb.WriteString("• <code>/schedule show</code> - View digest schedule\n")
	sb.WriteString("• <code>/schedule preview [count]</code> - Preview upcoming digest times\n")
	sb.WriteString("• <code>/schedule clear</code> - Clear current schedule\n")
	sb.WriteString("• <code>/language ru</code> - Set digest language\n\n")

	sb.WriteString("💡 <i>Tip: Use /settings to see all current values.</i>")

	b.reply(msg, sb.String())
}

func (b *Bot) handlePreview(ctx context.Context, msg *tgbotapi.Message) {
	if b.digestBuilder == nil {
		b.reply(msg, "❌ Digest preview is not available in this mode.")

		return
	}

	window, threshold := b.getPreviewParams(ctx)
	start, end := time.Now().Add(-window), time.Now()

	text, items, clusters, err := b.buildPreviewDigest(ctx, start, end, threshold)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error building digest preview: %s", html.EscapeString(err.Error())))

		return
	}

	if text == "" {
		b.reply(msg, "ℹ️ No items found for the current window to include in a digest.")

		return
	}

	header := fmt.Sprintf("📝 <b>Digest Preview</b> (%d items)\n<i>This has not been posted to the target channel.</i>\n\n", len(items))
	b.sendPreviewWithSettings(ctx, msg, header, text, items, clusters, start, end, threshold)
}

// getPreviewParams retrieves window and threshold settings for preview.
func (b *Bot) getPreviewParams(ctx context.Context) (time.Duration, float32) {
	windowStr := b.cfg.DigestWindow

	if err := b.database.GetSetting(ctx, SettingDigestWindow, &windowStr); err != nil {
		b.logger.Debug().Err(err).Msg("could not get SettingDigestWindow from DB")
	}

	window, err := time.ParseDuration(windowStr)
	if err != nil {
		window = time.Hour
	}

	threshold := b.cfg.ImportanceThreshold

	if err := b.database.GetSetting(ctx, SettingImportanceThreshold, &threshold); err != nil {
		b.logger.Debug().Err(err).Msg(MsgCouldNotGetImportanceThreshold)
	}

	return window, threshold
}

// buildPreviewDigest builds the digest text and returns items/clusters.
func (b *Bot) buildPreviewDigest(ctx context.Context, start, end time.Time, threshold float32) (string, []db.Item, []db.ClusterWithItems, error) {
	text, items, clusters, _, err := b.digestBuilder.BuildDigest(ctx, start, end, threshold, b.logger)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to build digest: %w", err)
	}

	return text, items, clusters, nil
}

// sendPreviewWithSettings sends preview based on current image settings.
func (b *Bot) sendPreviewWithSettings(ctx context.Context, msg *tgbotapi.Message, header, text string, items []db.Item, clusters []db.ClusterWithItems, start, end time.Time, threshold float32) {
	var inlineImagesEnabled bool

	if err := b.database.GetSetting(ctx, SettingDigestInlineImages, &inlineImagesEnabled); err != nil {
		b.logger.Debug().Err(err).Msg("could not get digest_inline_images from DB")
	}

	if inlineImagesEnabled {
		b.sendPreviewRichDigest(ctx, msg, header, text, items, start, end, threshold)

		return
	}

	b.sendPreviewWithCover(ctx, msg, header, text, items, clusters, start, end, threshold)
}

// sendPreviewWithCover sends preview with cover image if enabled.
func (b *Bot) sendPreviewWithCover(ctx context.Context, msg *tgbotapi.Message, header, text string, items []db.Item, clusters []db.ClusterWithItems, start, end time.Time, threshold float32) {
	coverImage := b.fetchPreviewCoverImage(ctx, items, clusters, start, end, threshold)

	if len(coverImage) > 0 {
		if _, err := b.SendDigestWithImage(ctx, msg.Chat.ID, header+text, "", coverImage); err != nil {
			b.logger.Warn().Err(err).Msg("failed to send preview with image, falling back to text")
			b.reply(msg, header+text)
		}

		return
	}

	b.reply(msg, header+text)
}

// sendPreviewRichDigest sends preview with inline images per item.
func (b *Bot) sendPreviewRichDigest(ctx context.Context, msg *tgbotapi.Message, header, text string, items []db.Item, start, end time.Time, threshold float32) {
	// Fetch items with media
	itemsWithMedia, err := b.database.GetItemsForWindowWithMedia(ctx, start, end, threshold, len(items))
	if err != nil {
		b.logger.Warn().Err(err).Msg("failed to fetch items with media for preview")
		b.reply(msg, header+text)

		return
	}

	// Build rich content
	richItems := make([]digest.RichDigestItem, 0, len(itemsWithMedia))

	for _, item := range itemsWithMedia {
		richItems = append(richItems, digest.RichDigestItem{
			Summary:    item.Summary,
			Topic:      item.Topic,
			Importance: item.ImportanceScore,
			Channel:    item.SourceChannel,
			ChannelID:  item.SourceChannelID,
			MsgID:      item.SourceMsgID,
			MediaData:  item.MediaData,
		})
	}

	content := digest.RichDigestContent{
		Header:   header,
		Items:    richItems,
		DigestID: "", // No rating buttons for preview
	}

	if _, err := b.SendRichDigest(ctx, msg.Chat.ID, content); err != nil {
		b.logger.Warn().Err(err).Msg("failed to send rich digest preview, falling back to text")
		b.reply(msg, header+text)
	}
}

// fetchPreviewCoverImage fetches or generates cover image for preview.
func (b *Bot) fetchPreviewCoverImage(ctx context.Context, items []db.Item, clusters []db.ClusterWithItems, start, end time.Time, threshold float32) []byte {
	var aiCoverEnabled bool

	if err := b.database.GetSetting(ctx, SettingDigestAICover, &aiCoverEnabled); err != nil {
		b.logger.Debug().Err(err).Msg("could not get digest_ai_cover from DB")
	}

	// Try AI cover first if enabled (independent of cover_image setting)
	if aiCoverEnabled && b.llmClient != nil {
		topics := extractTopicsForPreview(items, clusters)
		narrative := b.prepareNarrativeForPreview(ctx, items, clusters)

		coverImage, err := b.llmClient.GenerateDigestCover(ctx, topics, narrative)
		if err != nil {
			b.logger.Warn().Err(err).Msg("failed to generate AI cover for preview")
		} else {
			return coverImage
		}
	}

	// Check if regular cover image is enabled
	var coverImageEnabled = true

	if err := b.database.GetSetting(ctx, SettingDigestCoverImage, &coverImageEnabled); err != nil {
		b.logger.Debug().Err(err).Msg("could not get digest_cover_image from DB")
	}

	if !coverImageEnabled {
		return nil
	}

	// Fall back to DB cover image
	coverImage, err := b.database.GetDigestCoverImage(ctx, start, end, threshold)
	if err != nil {
		b.logger.Debug().Err(err).Msg("no cover image available for preview")

		return nil
	}

	return coverImage
}

// extractTopicsForPreview extracts unique topics from items and clusters.
func extractTopicsForPreview(items []db.Item, clusters []db.ClusterWithItems) []string {
	topicSet := make(map[string]struct{})

	for _, c := range clusters {
		if c.Topic != "" {
			topicSet[c.Topic] = struct{}{}
		}
	}

	for _, item := range items {
		if item.Topic != "" {
			topicSet[item.Topic] = struct{}{}
		}
	}

	topics := make([]string, 0, len(topicSet))
	for topic := range topicSet {
		topics = append(topics, topic)
	}

	return topics
}

// narrativeMaxItems is the maximum number of summaries to include in narrative.
const narrativeMaxItems = 5

// collectClusterSummariesForPreview extracts summaries from clusters up to maxItems.
func collectClusterSummariesForPreview(clusters []db.ClusterWithItems, maxItems int) []string {
	summaries := make([]string, 0, maxItems)

	for _, c := range clusters {
		if len(summaries) >= maxItems {
			break
		}

		if c.Topic != "" && len(c.Items) > 0 {
			summaries = append(summaries, c.Items[0].Summary)
		}
	}

	return summaries
}

// appendItemSummariesForPreview adds item summaries to existing slice up to maxItems.
func appendItemSummariesForPreview(summaries []string, items []db.Item, maxItems int) []string {
	for _, item := range items {
		if len(summaries) >= maxItems {
			break
		}

		if item.Summary != "" {
			summaries = append(summaries, item.Summary)
		}
	}

	return summaries
}

// prepareNarrativeForPreview prepares summaries for DALL-E by stripping HTML and compressing to short English phrases.
func (b *Bot) prepareNarrativeForPreview(ctx context.Context, items []db.Item, clusters []db.ClusterWithItems) string {
	// Get raw summaries
	summaries := collectClusterSummariesForPreview(clusters, narrativeMaxItems)
	summaries = appendItemSummariesForPreview(summaries, items, narrativeMaxItems)

	if len(summaries) == 0 {
		return ""
	}

	// Strip HTML tags from each summary
	cleanSummaries := make([]string, len(summaries))
	for i, summary := range summaries {
		cleanSummaries[i] = htmlutils.StripHTMLTags(summary)
	}

	// Compress summaries to short English phrases using LLM
	phrases, err := b.llmClient.CompressSummariesForCover(ctx, cleanSummaries)
	if err != nil {
		b.logger.Warn().Err(err).Msg("failed to compress summaries for preview, using raw text")

		return strings.Join(cleanSummaries, "; ")
	}

	if len(phrases) == 0 {
		return strings.Join(cleanSummaries, "; ")
	}

	return strings.Join(phrases, "; ")
}

func (b *Bot) handleTone(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.ToLower(msg.CommandArguments())

	if args != "professional" && args != "casual" && args != "brief" {
		b.reply(msg, "Usage: <code>/tone &lt;professional|casual|brief&gt;</code>")

		return
	}

	if err := b.database.SaveSettingWithHistory(ctx, "digest_tone", args, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving tone: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Digest tone set to <code>%s</code>.", html.EscapeString(args)))
}

func (b *Bot) handleDedup(ctx context.Context, msg *tgbotapi.Message) {
	args := msg.CommandArguments()

	if args != "strict" && args != "semantic" {
		b.reply(msg, "Usage: <code>/dedup &lt;strict|semantic&gt;</code>")

		return
	}

	if err := b.database.SaveSettingWithHistory(ctx, "dedup_mode", args, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error saving dedup mode: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Deduplication mode set to <code>%s</code>.", html.EscapeString(args)))
}

func (b *Bot) handleSettings(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) > 0 && args[0] == SubCmdReset {
		b.handleSettingsReset(ctx, msg, args)

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
		{SettingTargetChatID, "Target Chat ID", b.cfg.TargetChatID},
		{SettingDigestWindow, "Digest Window", b.cfg.DigestWindow},
		{SettingRelevanceThreshold, "Relevance Threshold", b.cfg.RelevanceThreshold},
		{SettingImportanceThreshold, "Importance Threshold", b.cfg.ImportanceThreshold},
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
		{SettingFiltersAds, "Ads Filter", false},
		{"filters_min_length", "Min Message Length", 20},
		{"filters_skip_forwards", "Skip Forwards", false},
		{SettingFiltersAdsKeywords, "Ads Keywords Count", 0},
		{SettingDiscoveryMinSeen, "Discovery Min Seen", DefaultDiscoveryMinSeen},
		{SettingDiscoveryMinScore, "Discovery Min Engagement", DefaultDiscoveryMinEngagement},
		{SettingDiscoveryAllow, "Discovery Allow Keywords Count", 0},
		{SettingDiscoveryDeny, "Discovery Deny Keywords Count", 0},
		{SettingDigestCoverImage, "Cover Image", true},
		{SettingDigestAICover, "AI Cover (DALL-E)", false},
		{SettingDigestInlineImages, "Inline Images", false},
		{"admin_ids", "Additional Admins", "none"},
	}

	for _, s := range settings {
		val := getSettingDisplayValue(s.key, s.def, dbSettings)
		sb.WriteString(fmt.Sprintf("• <b>%s:</b> <code>%v</code>\n", s.title, html.EscapeString(fmt.Sprintf("%v", val))))
	}

	sb.WriteString(fmt.Sprintf("• <b>Static Admins:</b> <code>%v</code>\n", html.EscapeString(fmt.Sprintf("%v", b.cfg.AdminIDs))))
	sb.WriteString(TipSettingsReset)

	b.reply(msg, sb.String())
}

// getSettingDisplayValue returns the display value for a setting, handling keyword arrays.
func getSettingDisplayValue(key string, def interface{}, dbSettings map[string]interface{}) interface{} {
	val, ok := dbSettings[key]
	if !ok {
		return def
	}

	if !isKeywordCountSetting(key) {
		return val
	}

	return countKeywordArray(val)
}

func isKeywordCountSetting(key string) bool {
	return key == SettingFiltersAdsKeywords || key == SettingDiscoveryAllow || key == SettingDiscoveryDeny
}

func countKeywordArray(val interface{}) int {
	if kwArr, ok := val.([]interface{}); ok {
		return len(kwArr)
	}

	if kwArr, ok := val.([]string); ok {
		return len(kwArr)
	}

	return 0
}

func (b *Bot) handleSettingsReset(ctx context.Context, msg *tgbotapi.Message, args []string) {
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
}

func (b *Bot) handleHelp(_ context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 {
		b.reply(msg, helpSummaryMessage())

		return
	}

	topic := strings.ToLower(args[0])
	helpMsgs := map[string]string{
		"all":         helpAllMessage(),
		"channels":    helpChannelsMessage(),
		CmdChannel:    helpChannelsMessage(),
		"discover":    helpDiscoverMessage(),
		"filters":     helpFiltersMessage(),
		"filter":      helpFiltersMessage(),
		"schedule":    helpScheduleMessage(),
		"config":      helpConfigMessage(),
		"ai":          helpAIMessage(),
		"enrichment":  enrichmentHelpMessage(),
		"system":      helpSystemMessage(),
		"research":    helpResearchMessage(),
		"scores":      helpScoresMessage(),
		"factcheck":   helpFactCheckMessage(),
		"ratings":     helpRatingsMessage(),
		"annotate":    helpAnnotateMessage(),
		"annotations": helpAnnotateMessage(),
		"botfather":   botFatherCommandsMessage(),
	}

	if m, ok := helpMsgs[topic]; ok {
		b.reply(msg, m)
	} else {
		b.reply(msg, fmt.Sprintf("❓ Unknown help topic: <code>%s</code>\n\n%s", html.EscapeString(topic), helpSummaryMessage()))
	}
}

func (b *Bot) handleBotFather(_ context.Context, msg *tgbotapi.Message) {
	b.reply(msg, botFatherCommandsMessage())
}

func (b *Bot) handleResearch(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 || strings.EqualFold(args[0], "help") {
		b.reply(msg, helpResearchMessage())
		return
	}

	switch {
	case strings.EqualFold(args[0], "login"):
		if b.cfg.ExpandedViewSigningSecret == "" || b.cfg.ExpandedViewBaseURL == "" {
			b.reply(msg, "❌ Research dashboard is not configured. Set EXPANDED_VIEW_SIGNING_SECRET and EXPANDED_VIEW_BASE_URL.")
			return
		}

		tokenService := research.NewAuthTokenService(b.cfg.ExpandedViewSigningSecret, research.DefaultLoginTokenTTL)

		token, err := tokenService.Generate(msg.From.ID)
		if err != nil {
			b.reply(msg, fmt.Sprintf("❌ Failed to generate login token: %s", html.EscapeString(err.Error())))
			return
		}

		baseURL := strings.TrimRight(b.cfg.ExpandedViewBaseURL, "/")
		loginURL := fmt.Sprintf("%s/research/login?token=%s", baseURL, url.QueryEscape(token))
		b.reply(msg, fmt.Sprintf("🔐 <b>Research Login</b>\n%s", html.EscapeString(loginURL)))
	case strings.EqualFold(args[0], "rebuild"):
		if err := b.database.RefreshResearchMaterializedViews(ctx); err != nil {
			b.reply(msg, fmt.Sprintf("❌ Research rebuild failed: %s", html.EscapeString(err.Error())))
			return
		}

		b.reply(msg, "✅ Research rebuild complete.")
	default:
		b.reply(msg, helpResearchMessage())
	}
}

func (b *Bot) handleErrors(ctx context.Context, msg *tgbotapi.Message) {
	var sb strings.Builder

	hasErrors := false

	// Pipeline processing errors
	errors, err := b.database.GetRecentErrors(ctx, RecentErrorsLimit)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error fetching errors: %s", html.EscapeString(err.Error())))

		return
	}

	if len(errors) > 0 {
		hasErrors = true

		sb.WriteString("⚠️ <b>Pipeline Processing Errors:</b>\n\n")

		for _, e := range errors {
			sb.WriteString(fmt.Sprintf("• <b>Channel:</b> %s\n", html.EscapeString(e.SourceChannel)))
			sb.WriteString(fmt.Sprintf("  <b>Error:</b> %s\n", b.humanizeError(e.ErrorJSON)))
			sb.WriteString(fmt.Sprintf("  <b>Time:</b> <code>%s</code>\n", e.CreatedAt.Format(DateTimeFormat)))
			sb.WriteString(fmt.Sprintf("  %s | /retry_%s\n\n", FormatLink(e.SourceChannel, e.SourceChannelID, e.SourceMsgID, "[View Message]"), strings.ReplaceAll(e.ID, "-", "")))
		}
	}

	// Enrichment queue errors
	enrichmentErrors, err := b.database.CountEnrichmentErrors(ctx)
	if err == nil && enrichmentErrors > 0 {
		hasErrors = true

		if sb.Len() > 0 {
			sb.WriteString("\n")
		}

		sb.WriteString(fmt.Sprintf("⚠️ <b>Enrichment Errors:</b> <code>%d</code> items\n", enrichmentErrors))
		sb.WriteString("Use <code>/retry enrichment</code> to requeue.\n")
	}

	if !hasErrors {
		b.reply(msg, "✅ No recent errors found.")

		return
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

func (b *Bot) handleHistory(ctx context.Context, msg *tgbotapi.Message) {
	history, err := b.database.GetRecentSettingHistory(ctx, SettingHistoryLimit)
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

		text += fmt.Sprintf("  🕒 %s\n", h.ChangedAt.Format(DateTimeFormat))
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

	text += TipSettingsReset
	b.reply(msg, text)
}

func (b *Bot) handleRetry(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.showRetryStatus(ctx, msg)

		return
	}

	switch strings.ToLower(args[0]) {
	case SubCmdConfirm:
		b.retryPipelineItems(ctx, msg)
	case "enrichment":
		b.handleRetryEnrichment(ctx, msg, args[1:])
	default:
		// Support both /retry ID and /retry_ID
		id := strings.TrimPrefix(args[0], "_")

		if err := b.database.RetryItem(ctx, id); err != nil {
			b.reply(msg, fmt.Sprintf("❌ Error retrying item %s: %s", html.EscapeString(id), html.EscapeString(err.Error())))

			return
		}

		b.reply(msg, fmt.Sprintf("✅ Item <code>%s</code> has been requeued.", html.EscapeString(id)))
	}
}

func (b *Bot) showRetryStatus(ctx context.Context, msg *tgbotapi.Message) {
	var sb strings.Builder

	pipelineErrors, _ := b.database.GetRecentErrors(ctx, RetryErrorsLimit) //nolint:errcheck // best-effort
	enrichmentErrors, _ := b.database.CountEnrichmentErrors(ctx)           //nolint:errcheck // best-effort

	if len(pipelineErrors) == 0 && enrichmentErrors == 0 {
		b.reply(msg, "✅ No failed items found to retry.")

		return
	}

	sb.WriteString("⚠️ <b>Failed Items Summary:</b>\n\n")

	if len(pipelineErrors) > 0 {
		sb.WriteString(fmt.Sprintf("• Pipeline errors: <code>%d</code>\n", len(pipelineErrors)))
		sb.WriteString("  → <code>/retry confirm</code> to requeue\n\n")
	}

	if enrichmentErrors > 0 {
		sb.WriteString(fmt.Sprintf("• Enrichment errors: <code>%d</code>\n", enrichmentErrors))
		sb.WriteString("  → <code>/retry enrichment confirm</code> to requeue\n")
	}

	b.reply(msg, sb.String())
}

func (b *Bot) retryPipelineItems(ctx context.Context, msg *tgbotapi.Message) {
	if err := b.database.RetryFailedItems(ctx); err != nil {
		b.reply(msg, fmt.Sprintf(fmtErrRetryingItems, html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, "✅ All failed pipeline items have been requeued for processing.")
}

func (b *Bot) handleRetryEnrichment(ctx context.Context, msg *tgbotapi.Message, args []string) {
	errorCount, err := b.database.CountEnrichmentErrors(ctx)
	if err != nil {
		b.reply(msg, fmt.Sprintf("❌ Error counting failed items: %s", html.EscapeString(err.Error())))

		return
	}

	if len(args) == 0 || strings.ToLower(args[0]) != SubCmdConfirm {
		if errorCount == 0 {
			b.reply(msg, "✅ No failed enrichment items found.")

			return
		}

		b.reply(msg, fmt.Sprintf("⚠️ <code>%d</code> failed enrichment items found.\n\nUse <code>/retry enrichment confirm</code> to requeue all.", errorCount))

		return
	}

	if errorCount == 0 {
		b.reply(msg, "✅ No failed enrichment items to retry.")

		return
	}

	requeued, err := b.database.RetryFailedEnrichmentItems(ctx)
	if err != nil {
		b.reply(msg, fmt.Sprintf(fmtErrRetryingItems, html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("✅ Requeued <code>%d</code> enrichment items for processing.", requeued))
}
