package bot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const discoveryListTip = "\U0001F4A1 <i>Use <code>/discover approve @username</code> or <code>/discover reject @username</code></i>"

func (b *Bot) handleDiscoverNamespace(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())

	if len(args) == 0 {
		b.handleDiscoverList(ctx, msg)

		return
	}

	subcommand := args[0]

	if b.dispatchDiscoverSubcommand(ctx, msg, args, subcommand) {
		return
	}

	b.reply(msg, discoverUnknownSubcommandMsg(subcommand))
}

func (b *Bot) dispatchDiscoverSubcommand(ctx context.Context, msg *tgbotapi.Message, args []string, subcommand string) bool {
	// Normalize aliases to canonical commands
	canonical := normalizeDiscoverSubcommand(subcommand)

	return b.executeDiscoverSubcommand(ctx, msg, args, canonical)
}

func normalizeDiscoverSubcommand(subcommand string) string {
	aliases := map[string]string{
		"ignore":        SubCmdReject,
		"rejected":      SubCmdRejected,
		"minseen":       "min_seen",
		"minengagement": "min_engagement",
	}

	if canonical, ok := aliases[subcommand]; ok {
		return canonical
	}

	return subcommand
}

func (b *Bot) executeDiscoverSubcommand(ctx context.Context, msg *tgbotapi.Message, args []string, cmd string) bool {
	if b.executeDiscoverSimpleCmd(ctx, msg, cmd) {
		return true
	}

	return b.executeDiscoverArgsCmd(ctx, msg, args, cmd)
}

func (b *Bot) executeDiscoverSimpleCmd(ctx context.Context, msg *tgbotapi.Message, cmd string) bool {
	switch cmd {
	case SubCmdCleanup:
		b.handleDiscoverCleanup(ctx, msg)
	case SubCmdStats:
		b.handleDiscoverStats(ctx, msg)
	case subCmdHelp:
		b.reply(msg, discoverHelpMessage())
	default:
		return false
	}

	return true
}

func discoverHelpMessage() string {
	return "\U0001F4D6 <b>Discovery Commands</b>\n\n" +
		"<b>Browse discoveries:</b>\n" +
		"\u2022 <code>/discover</code> - List pending discoveries\n" +
		"\u2022 <code>/discover stats</code> - Show statistics\n\n" +
		"<b>Manage channels:</b>\n" +
		"\u2022 <code>/discover approve @user</code> - Add to tracking\n" +
		"\u2022 <code>/discover reject @user</code> - Mark as not useful\n" +
		"\u2022 <code>/discover preview @user</code> - Check why visible/hidden\n\n" +
		"<b>Threshold filters:</b>\n" +
		"\u2022 <code>/discover min_seen &lt;n&gt;</code> - Min discovery count\n" +
		"\u2022 <code>/discover min_engagement &lt;n&gt;</code> - Min engagement score\n\n" +
		"<b>Keyword filters:</b>\n" +
		"\u2022 <code>/discover allow</code> - List allow keywords\n" +
		"\u2022 <code>/discover allow &lt;word&gt;</code> - Add allow keyword\n" +
		"\u2022 <code>/discover allow remove &lt;word&gt;</code> - Remove keyword\n" +
		"\u2022 <code>/discover deny</code> - List deny keywords\n" +
		"\u2022 <code>/discover deny &lt;word&gt;</code> - Add deny keyword\n\n" +
		"<b>Maintenance:</b>\n" +
		"\u2022 <code>/discover cleanup</code> - Backfill matched channels\n" +
		"\u2022 <code>/discover rejected</code> - Show rejected list"
}

func (b *Bot) executeDiscoverArgsCmd(ctx context.Context, msg *tgbotapi.Message, args []string, cmd string) bool {
	switch cmd {
	case SubCmdApprove:
		b.handleDiscoverApproveCmd(ctx, msg, args)
	case SubCmdReject:
		b.handleDiscoverRejectCmd(ctx, msg, args)
	case SubCmdRejected:
		b.handleDiscoverShowRejectedCmd(ctx, msg, args)
	case SubCmdPreview:
		b.handleDiscoverPreviewCmd(ctx, msg, args)
	case "min_seen":
		b.handleDiscoverMinSeen(ctx, msg, args)
	case "min_engagement":
		b.handleDiscoverMinEngagement(ctx, msg, args)
	case filterTypeAllow:
		b.handleDiscoverKeywordCmd(ctx, msg, args, SettingDiscoveryAllow, filterTypeAllow)
	case filterTypeDeny:
		b.handleDiscoverKeywordCmd(ctx, msg, args, SettingDiscoveryDeny, filterTypeDeny)
	default:
		return false
	}

	return true
}

func discoverUnknownSubcommandMsg(subcommand string) string {
	return "\u2753 Unknown subcommand: <code>" + html.EscapeString(subcommand) + "</code>\n\n" +
		"<b>Available:</b>\n" +
		"\u2022 <code>approve @user</code> - Add to tracking\n" +
		"\u2022 <code>reject @user</code> - Mark as not useful\n" +
		"\u2022 <code>ignore @user</code> - Alias for reject\n" +
		"\u2022 <code>rejected</code> - Show rejected list\n" +
		"\u2022 <code>preview @user</code> - Why is/isn't actionable\n" +
		"\u2022 <code>allow</code> - Manage allow keywords\n" +
		"\u2022 <code>deny</code> - Manage deny keywords\n" +
		"\u2022 <code>stats</code> - Discovery statistics\n" +
		"\u2022 <code>cleanup</code> - Backfill matched channels"
}

func (b *Bot) handleDiscoverMinSeen(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) < 2 {
		b.reply(msg, "Usage: <code>/discover min_seen &lt;number&gt;</code>")

		return
	}

	val, err := strconv.Atoi(args[1])
	if err != nil || val < 1 {
		b.reply(msg, "\u274C Invalid value. Please provide a positive number >= 1.")

		return
	}

	if err := b.database.SaveSettingWithHistory(ctx, SettingDiscoveryMinSeen, val, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("\u274C Error saving discovery_min_seen: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("\u2705 Discovery minimum seen count updated to <code>%d</code>.", val))
}

func (b *Bot) handleDiscoverMinEngagement(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) < 2 {
		b.reply(msg, "Usage: <code>/discover min_engagement &lt;number&gt;</code>")

		return
	}

	val, err := strconv.ParseFloat(args[1], 32)
	if err != nil || val < 0 {
		b.reply(msg, "\u274C Invalid value. Please provide a non-negative number.")

		return
	}

	if err := b.database.SaveSettingWithHistory(ctx, SettingDiscoveryMinScore, float32(val), msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("\u274C Error saving discovery_min_engagement: %s", html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("\u2705 Discovery minimum engagement updated to <code>%v</code>.", val))
}

func (b *Bot) handleDiscoverKeywordCmd(ctx context.Context, msg *tgbotapi.Message, args []string, settingKey, label string) {
	keywords, err := b.getDiscoveryKeywordList(ctx, settingKey)
	if err != nil {
		b.reply(msg, fmt.Sprintf("\u274C Error fetching discovery %s keywords: %s", html.EscapeString(label), html.EscapeString(err.Error())))

		return
	}

	if len(args) < 2 {
		if len(keywords) == 0 {
			b.reply(msg, fmt.Sprintf("\U0001F4CB No discovery %s keywords configured.", html.EscapeString(label)))

			return
		}

		b.reply(msg, fmt.Sprintf("\U0001F4CB <b>Discovery %s keywords:</b>\n<code>%s</code>\n\nUsage: <code>/discover %s add &lt;word&gt;</code> | <code>/discover %s remove &lt;word&gt;</code> | <code>/discover %s clear</code>", html.EscapeString(label), html.EscapeString(strings.Join(keywords, ", ")), html.EscapeString(label), html.EscapeString(label), html.EscapeString(label)))

		return
	}

	action := strings.ToLower(args[1])

	updated, ok := b.processDiscoveryKeywordAction(msg, label, action, args[2:], keywords)
	if !ok {
		return
	}

	updated = db.NormalizeDiscoveryKeywords(updated)

	if err := b.database.SaveSettingWithHistory(ctx, settingKey, updated, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("\u274C Error saving discovery %s keywords: %s", html.EscapeString(label), html.EscapeString(err.Error())))

		return
	}

	b.reply(msg, fmt.Sprintf("\u2705 Discovery %s keywords updated. Total: <code>%d</code>.", html.EscapeString(label), len(updated)))
}

func (b *Bot) processDiscoveryKeywordAction(msg *tgbotapi.Message, label, action string, args []string, keywords []string) ([]string, bool) {
	switch action {
	case CmdAdd:
		return b.addDiscoveryKeyword(msg, label, args, keywords)
	case CmdRemove:
		return b.removeDiscoveryKeyword(msg, label, args, keywords)
	case SubCmdClear:
		return []string{}, true
	default:
		// Treat unknown action as a keyword to add (e.g., "/discover deny word" -> add "word")
		allArgs := append([]string{action}, args...)

		return b.addDiscoveryKeyword(msg, label, allArgs, keywords)
	}
}

func (b *Bot) addDiscoveryKeyword(msg *tgbotapi.Message, label string, args []string, keywords []string) ([]string, bool) {
	keyword := strings.TrimSpace(strings.ToLower(strings.Join(args, " ")))
	if keyword == "" {
		b.reply(msg, fmt.Sprintf("Usage: <code>/discover %s add &lt;word&gt;</code>", html.EscapeString(label)))

		return nil, false
	}

	for _, existing := range keywords {
		if existing == keyword {
			b.reply(msg, errKeywordAlreadyExists)

			return nil, false
		}
	}

	return append(keywords, keyword), true
}

func (b *Bot) removeDiscoveryKeyword(msg *tgbotapi.Message, label string, args []string, keywords []string) ([]string, bool) {
	keyword := strings.TrimSpace(strings.ToLower(strings.Join(args, " ")))
	if keyword == "" {
		b.reply(msg, fmt.Sprintf("Usage: <code>/discover %s remove &lt;word&gt;</code>", html.EscapeString(label)))

		return nil, false
	}

	updated := make([]string, 0, len(keywords))
	found := false

	for _, existing := range keywords {
		if existing == keyword {
			found = true
			continue
		}

		updated = append(updated, existing)
	}

	if !found {
		b.reply(msg, errKeywordNotFound)

		return nil, false
	}

	return updated, true
}

func (b *Bot) handleDiscoverApproveCmd(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) < 2 {
		b.reply(msg, "Usage: <code>/discover approve &lt;@username&gt;</code>")

		return
	}

	b.handleDiscoverApprove(ctx, msg, args[1])
}

func (b *Bot) handleDiscoverRejectCmd(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) < 2 {
		b.reply(msg, "Usage: <code>/discover reject &lt;@username&gt;</code>")

		return
	}

	b.handleDiscoverReject(ctx, msg, args[1])
}

func (b *Bot) handleDiscoverShowRejectedCmd(ctx context.Context, msg *tgbotapi.Message, args []string) {
	limit := DiscoveriesLimit

	if len(args) > 1 {
		if v, err := strconv.Atoi(args[1]); err == nil && v > 0 {
			limit = v
		} else {
			b.reply(msg, "Usage: <code>/discover show-rejected [limit]</code>")

			return
		}
	}

	b.handleDiscoverShowRejected(ctx, msg, limit)
}

func (b *Bot) handleDiscoverPreviewCmd(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) < 2 {
		b.reply(msg, discoverPreviewUsage)

		return
	}

	b.handleDiscoverPreview(ctx, msg, args[1])
}

func (b *Bot) handleDiscoverList(ctx context.Context, msg *tgbotapi.Message) {
	minSeen, minEngagement := b.getDiscoveryThresholds(ctx)

	discoveries, err := b.database.GetPendingDiscoveries(ctx, DiscoveriesLimit, minSeen, minEngagement)
	if err != nil {
		b.reply(msg, fmt.Sprintf("\u274C Error fetching discoveries: %s", html.EscapeString(err.Error())))

		return
	}

	allow, deny := b.getDiscoveryKeywordFilters(ctx)
	filtered, allowMiss, denyHit := db.FilterDiscoveriesByKeywords(discoveries, allow, deny)
	filterNote := formatDiscoveryFilterNote(minSeen, minEngagement, allow, deny, allowMiss, denyHit)

	if len(filtered) == 0 {
		noPending := "\U0001F4CB No pending channel discoveries.\n\n" + filterNote + "\n\n\U0001F4A1 Channels are discovered from forwards, t.me links, and @mentions."
		b.reply(msg, noPending)

		return
	}

	tip := discoveryListTip
	if filterNote != "" {
		tip = tip + "\n\n" + filterNote
	}

	text := formatDiscoveryListWithTip(formatDiscoveryListTitle(minSeen, minEngagement), filtered, tip)
	rows := buildDiscoveryKeyboard(filtered)

	reply := tgbotapi.NewMessage(msg.Chat.ID, text)
	reply.ParseMode = tgbotapi.ModeHTML

	if len(rows) > 0 {
		reply.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	}

	if _, err := b.api.Send(reply); err != nil {
		b.logger.Error().Err(err).Msg("failed to send discover list")
	}
}

func (b *Bot) getDiscoveryThresholds(ctx context.Context) (int, float32) {
	minSeen := DefaultDiscoveryMinSeen
	if err := b.database.GetSetting(ctx, SettingDiscoveryMinSeen, &minSeen); err != nil {
		b.logger.Warn().Err(err).Msg("failed to read discovery_min_seen")
	}

	if minSeen < 1 {
		minSeen = DefaultDiscoveryMinSeen
	}

	minEngagement := DefaultDiscoveryMinEngagement
	if err := b.database.GetSetting(ctx, SettingDiscoveryMinScore, &minEngagement); err != nil {
		b.logger.Warn().Err(err).Msg("failed to read discovery_min_engagement")
	}

	if minEngagement < 0 {
		minEngagement = DefaultDiscoveryMinEngagement
	}

	return minSeen, minEngagement
}

func (b *Bot) getDiscoveryKeywordList(ctx context.Context, key string) ([]string, error) {
	var keywords []string

	if err := b.database.GetSetting(ctx, key, &keywords); err != nil {
		return nil, fmt.Errorf("get discovery keyword list %s: %w", key, err)
	}

	return db.NormalizeDiscoveryKeywords(keywords), nil
}

func (b *Bot) getDiscoveryKeywordFilters(ctx context.Context) ([]string, []string) {
	allow, err := b.getDiscoveryKeywordList(ctx, SettingDiscoveryAllow)
	if err != nil {
		b.logger.Warn().Err(err).Msg("failed to read discovery_description_allow")

		allow = nil
	}

	deny, err := b.getDiscoveryKeywordList(ctx, SettingDiscoveryDeny)
	if err != nil {
		b.logger.Warn().Err(err).Msg("failed to read discovery_description_deny")

		deny = nil
	}

	return allow, deny
}

func (b *Bot) getDiscoveryKeywordFilterStats(ctx context.Context, minSeen int, minEngagement float32, allow, deny []string) (int, int, int) {
	candidates, err := b.database.GetPendingDiscoveriesForFiltering(ctx, minSeen, minEngagement)
	if err != nil {
		b.logger.Warn().Err(err).Msg("failed to fetch discovery keyword candidates")

		return 0, 0, 0
	}

	if len(allow) == 0 && len(deny) == 0 {
		return len(candidates), 0, 0
	}

	_, allowMiss, denyHit := db.FilterDiscoveriesByKeywords(candidates, allow, deny)

	return len(candidates), allowMiss, denyHit
}

func (b *Bot) handleDiscoverPreview(ctx context.Context, msg *tgbotapi.Message, identifier string) {
	username := strings.ToLower(strings.TrimPrefix(identifier, "@"))
	if username == "" {
		b.reply(msg, discoverPreviewUsage)

		return
	}

	discovery, err := b.database.GetDiscoveryByUsername(ctx, username)
	if errors.Is(err, db.ErrDiscoveryNotFound) {
		b.reply(msg, fmt.Sprintf("\u274C No discovery record found for <code>@%s</code>.", html.EscapeString(username)))

		return
	}

	if err != nil {
		b.reply(msg, fmt.Sprintf("\u274C Error fetching discovery: %s", html.EscapeString(err.Error())))

		return
	}

	minSeen, minEngagement := b.getDiscoveryThresholds(ctx)
	allow, deny := b.getDiscoveryKeywordFilters(ctx)

	alreadyTracked, err := b.database.IsChannelTracked(ctx, discovery.Username, discovery.TGPeerID, discovery.InviteLink)
	if err != nil {
		b.reply(msg, fmt.Sprintf("\u274C Error checking channel status: %s", html.EscapeString(err.Error())))

		return
	}

	allowMatch, denyMatch, _ := db.EvaluateDiscoveryKeywords(*discovery, allow, deny)
	allowMiss := len(allow) > 0 && !allowMatch
	denyHit := denyMatch

	actionable, reasons := buildDiscoveryActionability(*discovery, minSeen, minEngagement, alreadyTracked, allowMiss, denyHit)

	b.reply(msg, formatDiscoveryPreview(*discovery, minSeen, minEngagement, allow, deny, allowMatch, denyMatch, alreadyTracked, actionable, reasons))
}

func (b *Bot) handleDiscoverShowRejected(ctx context.Context, msg *tgbotapi.Message, limit int) {
	discoveries, err := b.database.GetRejectedDiscoveries(ctx, limit)
	if err != nil {
		b.reply(msg, fmt.Sprintf("\u274C Error fetching rejected discoveries: %s", html.EscapeString(err.Error())))

		return
	}

	if len(discoveries) == 0 {
		b.reply(msg, "\U0001F4CB No rejected channel discoveries.")

		return
	}

	header := "\U0001F5C2 <b>Rejected Channel Discoveries</b>"
	if stats, err := b.database.GetDiscoveryStats(ctx); err == nil {
		header = fmt.Sprintf("%s (%d shown of %d)", header, len(discoveries), stats.RejectedCount)
	}

	text := formatDiscoveryListWithTip(header, discoveries, "\U0001F4A1 <i>Use <code>/discover approve @username</code> to add a channel back.</i>")
	b.sendMessage(msg.Chat.ID, text)
}

func (b *Bot) handleDiscoverCleanup(ctx context.Context, msg *tgbotapi.Message) {
	var (
		totalUpdated int
		batches      int
	)

	for batches < 100 {
		updated, err := b.database.CleanupDiscoveriesBatch(ctx, DiscoveryCleanupBatchSize, msg.From.ID)
		if err != nil {
			b.logger.Error().Err(err).Msg("discover cleanup batch failed")

			batches++

			continue
		}

		if updated == 0 {
			break
		}

		totalUpdated += updated
		batches++
	}

	b.reply(msg, fmt.Sprintf("\u2705 Cleanup complete. Updated <code>%d</code> discoveries, matched <code>%d</code> to tracked channels.", totalUpdated, totalUpdated))
}

func formatDiscoveryList(discoveries []db.DiscoveredChannel) string {
	return formatDiscoveryListWithTip("\U0001F50D <b>Pending Channel Discoveries</b>", discoveries, discoveryListTip)
}

func formatDiscoveryListTitle(minSeen int, minEngagement float32) string {
	return fmt.Sprintf("\U0001F50D <b>Pending Channel Discoveries</b> <i>(seen \u2265 %d, engagement \u2265 %.0f)</i>", minSeen, minEngagement)
}

func formatDiscoveryListWithTip(title string, discoveries []db.DiscoveredChannel, tip string) string {
	var sb strings.Builder

	sb.WriteString(title)
	sb.WriteString("\n\n")

	for _, d := range discoveries {
		sb.WriteString(formatDiscoveryItem(d))
	}

	if tip != "" {
		sb.WriteString("\n")
		sb.WriteString(tip)
	}

	return sb.String()
}

func formatDiscoveryFilterNote(minSeen int, minEngagement float32, allow, deny []string, allowMiss, denyHit int) string {
	note := fmt.Sprintf("<i>Filters: seen \u2265 %d, engagement \u2265 %.0f</i>", minSeen, minEngagement)

	if len(allow) > 0 || len(deny) > 0 {
		note = fmt.Sprintf("%s\n<i>Keyword filters: allow %d, deny %d", note, len(allow), len(deny))

		if allowMiss > 0 || denyHit > 0 {
			note += fmt.Sprintf(" (hidden: allow miss %d, deny hit %d)", allowMiss, denyHit)
		}

		note += htmlItalicClose
	}

	return note
}

func formatDiscoveryItem(d db.DiscoveredChannel) string {
	identifier := formatDiscoveryIdentifier(d)

	title := d.Title
	if title == "" {
		title = discoveryUnknown
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf(discoveryTitleIdentFmt, html.EscapeString(title), html.EscapeString(identifier)))

	infoLine := fmt.Sprintf("  Source: %s | Seen: %dx", d.SourceType, d.DiscoveryCount)
	if d.MaxViews > 0 || d.MaxForwards > 0 {
		infoLine += fmt.Sprintf(" | Engagement: %dv/%df", d.MaxViews, d.MaxForwards)
	}

	infoLine += fmt.Sprintf(" | Last: %s\n\n", d.LastSeenAt.Format(discoveryLastSeenFormat))
	sb.WriteString(infoLine)

	return sb.String()
}

func formatDiscoveryIdentifier(d db.DiscoveredChannel) string {
	if d.Username != "" {
		return "@" + d.Username
	}

	if d.TGPeerID != 0 {
		return fmt.Sprintf("ID:%d", d.TGPeerID)
	}

	if d.InviteLink != "" {
		return "[invite link]"
	}

	return ""
}

func buildDiscoveryActionability(d db.DiscoveredChannel, minSeen int, minEngagement float32, alreadyTracked, allowMiss, denyHit bool) (bool, []string) {
	status := d.Status
	if status == "" {
		status = discoveryUnknownLC
	}

	actionable := true

	var reasons []string

	if status != db.DiscoveryStatusPending {
		reasons = append(reasons, "status="+status)
		actionable = false
	}

	if d.MatchedChannelID != "" {
		reasons = append(reasons, "matched channel")
		actionable = false
	}

	if d.DiscoveryCount < minSeen || d.EngagementScore < float64(minEngagement) {
		reasons = append(reasons, "below thresholds")
		actionable = false
	}

	if alreadyTracked {
		reasons = append(reasons, "already tracked")
		actionable = false
	}

	if denyHit {
		reasons = append(reasons, "deny keyword hit")
		actionable = false
	}

	if allowMiss {
		reasons = append(reasons, "allow keywords missing")
		actionable = false
	}

	return actionable, reasons
}

func formatDiscoveryPreview(d db.DiscoveredChannel, minSeen int, minEngagement float32, allow, deny []string, allowMatch, denyMatch, alreadyTracked, actionable bool, reasons []string) string {
	title, identifier, status := extractDiscoveryFields(d)

	var sb strings.Builder

	sb.WriteString("\U0001F50D <b>Discovery Preview</b>\n\n")
	sb.WriteString(fmt.Sprintf(discoveryTitleIdentFmt, html.EscapeString(title), html.EscapeString(identifier)))
	sb.WriteString(fmt.Sprintf("  Status: <code>%s</code>\n", html.EscapeString(status)))

	writeDiscoveryPreviewDetails(&sb, d, minSeen, minEngagement, allow, deny, allowMatch, denyMatch, alreadyTracked)
	writeActionabilityLine(&sb, actionable, reasons)

	return sb.String()
}

func extractDiscoveryFields(d db.DiscoveredChannel) (title, identifier, status string) {
	title = d.Title
	if title == "" {
		title = discoveryUnknown
	}

	identifier = formatDiscoveryIdentifier(d)
	if identifier == "" && d.Username != "" {
		identifier = "@" + d.Username
	}

	status = d.Status
	if status == "" {
		status = discoveryUnknownLC
	}

	return title, identifier, status
}

func writeDiscoveryPreviewDetails(sb *strings.Builder, d db.DiscoveredChannel, minSeen int, minEngagement float32, allow, deny []string, allowMatch, denyMatch, alreadyTracked bool) {
	writeDiscoverySourceAndStats(sb, d, minSeen, minEngagement)
	writeDiscoveryMatchStatus(sb, d, alreadyTracked)
	writeDiscoveryKeywordStatus(sb, allow, deny, allowMatch, denyMatch)
	sb.WriteString("\n")
}

func writeDiscoverySourceAndStats(sb *strings.Builder, d db.DiscoveredChannel, minSeen int, minEngagement float32) {
	if d.SourceType != "" {
		fmt.Fprintf(sb, "  Source: <code>%s</code>\n", html.EscapeString(d.SourceType))
	}

	fmt.Fprintf(sb, "  Seen: <code>%d</code> (min %d)\n", d.DiscoveryCount, minSeen)

	if d.MaxViews > 0 || d.MaxForwards > 0 {
		fmt.Fprintf(sb, "  Engagement: <code>%dv/%df</code> (score %.1f, min %.0f)\n", d.MaxViews, d.MaxForwards, d.EngagementScore, minEngagement)
	} else {
		fmt.Fprintf(sb, "  Engagement score: <code>%.1f</code> (min %.0f)\n", d.EngagementScore, minEngagement)
	}

	if !d.LastSeenAt.IsZero() {
		fmt.Fprintf(sb, "  Last seen: <code>%s</code>\n", d.LastSeenAt.Format(discoveryLastSeenFormat))
	}
}

func writeDiscoveryMatchStatus(sb *strings.Builder, d db.DiscoveredChannel, alreadyTracked bool) {
	if d.MatchedChannelID != "" {
		fmt.Fprintf(sb, "  Matched channel: <code>yes</code> (<code>%s</code>)\n", html.EscapeString(d.MatchedChannelID))
	} else {
		sb.WriteString("  Matched channel: <code>no</code>\n")
	}

	trackedLine := previewNo
	if alreadyTracked {
		trackedLine = previewYes
	}

	fmt.Fprintf(sb, "  Already tracked: <code>%s</code>\n", trackedLine)
}

func writeDiscoveryKeywordStatus(sb *strings.Builder, allow, deny []string, allowMatch, denyMatch bool) {
	if len(allow) > 0 {
		matchLine := previewNo
		if allowMatch {
			matchLine = previewYes
		}

		fmt.Fprintf(sb, "  Allow keywords: <code>%d</code> (match <code>%s</code>)\n", len(allow), matchLine)
	}

	if len(deny) > 0 {
		hitLine := previewNo
		if denyMatch {
			hitLine = previewYes
		}

		fmt.Fprintf(sb, "  Deny keywords: <code>%d</code> (hit <code>%s</code>)\n", len(deny), hitLine)
	}
}

func writeActionabilityLine(sb *strings.Builder, actionable bool, reasons []string) {
	if actionable {
		sb.WriteString("\u2705 Actionable. This discovery should appear in <code>/discover</code>.\n")

		return
	}

	sb.WriteString("\u274C Not actionable.")

	if len(reasons) > 0 {
		fmt.Fprintf(sb, " Reasons: <code>%s</code>.\n", html.EscapeString(strings.Join(reasons, ", ")))
	} else {
		sb.WriteString("\n")
	}
}

func buildDiscoveryKeyboard(discoveries []db.DiscoveredChannel) [][]tgbotapi.InlineKeyboardButton {
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, d := range discoveries {
		if d.Username != "" {
			row := tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("\u2705 "+d.Username, "discover:approve:"+d.Username),
				tgbotapi.NewInlineKeyboardButtonData("\u274C "+d.Username, "discover:reject:"+d.Username),
			)
			rows = append(rows, row)
		}
	}

	return rows
}

func (b *Bot) handleDiscoverApprove(ctx context.Context, msg *tgbotapi.Message, username string) {
	username = strings.TrimPrefix(username, "@")

	if err := b.database.ApproveDiscovery(ctx, username, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("\u274C Error approving channel: %s", html.EscapeString(err.Error())))

		return
	}

	observability.DiscoveryApprovedTotal.Inc()
	b.reply(msg, fmt.Sprintf("\u2705 Channel <code>@%s</code> approved and added to active tracking.", html.EscapeString(username)))
}

func (b *Bot) handleDiscoverReject(ctx context.Context, msg *tgbotapi.Message, username string) {
	username = strings.TrimPrefix(username, "@")

	b.logger.Info().Str(LogFieldUsername, username).Int64(LogFieldUserID, msg.From.ID).Msg("rejecting discovery")

	if err := b.database.RejectDiscovery(ctx, username, msg.From.ID); err != nil {
		b.logger.Error().Err(err).Str(LogFieldUsername, username).Msg("failed to reject discovery")
		b.reply(msg, fmt.Sprintf("\u274C Error rejecting channel: %s", html.EscapeString(err.Error())))

		return
	}

	b.logger.Info().Str(LogFieldUsername, username).Msg("discovery rejected successfully")
	observability.DiscoveryRejectedTotal.Inc()
	b.reply(msg, fmt.Sprintf("\u2705 Channel <code>@%s</code> rejected. It will not appear in discoveries again.", html.EscapeString(username)))
}

func (b *Bot) handleDiscoverStats(ctx context.Context, msg *tgbotapi.Message) {
	stats, err := b.database.GetDiscoveryStats(ctx)
	if err != nil {
		b.reply(msg, fmt.Sprintf("\u274C Error fetching discovery stats: %s", html.EscapeString(err.Error())))

		return
	}

	minSeen, minEngagement := b.getDiscoveryThresholds(ctx)

	filterStats, err := b.database.GetDiscoveryFilterStats(ctx, minSeen, minEngagement)
	if err != nil {
		b.logger.Warn().Err(err).Msg("failed to fetch discovery filter stats")

		filterStats = nil
	}

	var sb strings.Builder

	sb.WriteString("\U0001F4CA <b>Channel Discovery Statistics</b>\n\n")
	sb.WriteString(fmt.Sprintf("\u2022 <b>Pending (raw):</b> <code>%d</code>\n", stats.PendingCount))

	if stats.UnresolvedCount > 0 {
		sb.WriteString(fmt.Sprintf("\u2022 <b>Unresolved:</b> <code>%d</code> <i>(peer ID only)</i>\n", stats.UnresolvedCount))
	}

	sb.WriteString(fmt.Sprintf("\u2022 <b>Rejected:</b> <code>%d</code>\n", stats.RejectedCount))
	sb.WriteString(fmt.Sprintf("\u2022 <b>Added:</b> <code>%d</code>\n", stats.AddedCount))
	sb.WriteString(fmt.Sprintf("\u2022 <b>Total Channels:</b> <code>%d</code>\n", stats.TotalCount))
	sb.WriteString(fmt.Sprintf("\u2022 <b>Total Discovery Events:</b> <code>%d</code>\n", stats.TotalDiscoveries))
	sb.WriteString(fmt.Sprintf("\u2022 <b>Thresholds:</b> seen \u2265 <code>%d</code>, engagement \u2265 <code>%.0f</code>\n", minSeen, minEngagement))

	if filterStats != nil {
		allow, deny := b.getDiscoveryKeywordFilters(ctx)
		candidateCount, allowMiss, denyHit := b.getDiscoveryKeywordFilterStats(ctx, minSeen, minEngagement, allow, deny)

		actionable := int64(candidateCount - allowMiss - denyHit)
		if actionable < 0 {
			actionable = 0
		}

		sb.WriteString(fmt.Sprintf("\u2022 <b>Pending (actionable):</b> <code>%d</code>\n", actionable))
		sb.WriteString("\n<b>Filtered (pending)</b>\n")
		sb.WriteString(fmt.Sprintf("\u2022 <b>Matched channel:</b> <code>%d</code>\n", filterStats.MatchedChannelIDCount))
		sb.WriteString(fmt.Sprintf("\u2022 <b>Below thresholds:</b> <code>%d</code>\n", filterStats.BelowThresholdCount))
		sb.WriteString(fmt.Sprintf("\u2022 <b>Already tracked:</b> <code>%d</code>\n", filterStats.AlreadyTrackedCount))
		sb.WriteString(fmt.Sprintf("\u2022 <b>Allow miss:</b> <code>%d</code>\n", allowMiss))
		sb.WriteString(fmt.Sprintf("\u2022 <b>Deny hit:</b> <code>%d</code>\n", denyHit))
	}

	b.reply(msg, sb.String())
}

func (b *Bot) handleDiscoverCallback(ctx context.Context, query *tgbotapi.CallbackQuery) {
	parts := strings.Split(query.Data, ":")

	if len(parts) != 3 {
		return
	}

	action := parts[1] // "approve" or "reject"
	username := parts[2]

	b.logger.Info().Str(LogFieldAction, action).Str(LogFieldUsername, username).Int64(LogFieldUserID, query.From.ID).Msg("discover callback received")

	var callbackText string

	var err error

	switch action {
	case SubCmdApprove:
		err = b.database.ApproveDiscovery(ctx, username, query.From.ID)
		if err == nil {
			callbackText = fmt.Sprintf("\u2705 @%s approved and added to tracking", username)

			observability.DiscoveryApprovedTotal.Inc()
			b.logger.Info().Str(LogFieldUsername, username).Msg("discovery approved via callback")
		}
	case SubCmdReject:
		err = b.database.RejectDiscovery(ctx, username, query.From.ID)
		if err == nil {
			callbackText = fmt.Sprintf("\u274C @%s rejected", username)

			observability.DiscoveryRejectedTotal.Inc()
			b.logger.Info().Str(LogFieldUsername, username).Msg("discovery rejected via callback")
		}
	default:
		return
	}

	if err != nil {
		callbackText = fmt.Sprintf(ErrGenericFmt, err.Error())
		b.logger.Error().Err(err).Str(LogFieldAction, action).Str(LogFieldUsername, username).Msg("discover callback failed")
	}

	callback := tgbotapi.NewCallback(query.ID, callbackText)
	callback.ShowAlert = true

	if _, err := b.api.Request(callback); err != nil {
		b.logger.Error().Err(err).Msg("failed to send callback response")
	}
}
