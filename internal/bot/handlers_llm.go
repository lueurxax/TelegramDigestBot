package bot

import (
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// LLM task names for model overrides.
var llmTaskSettings = map[string]string{
	"summarize": SettingLLMOverrideSummarize,
	"cluster":   SettingLLMOverrideCluster,
	"narrative": SettingLLMOverrideNarrative,
	"topic":     SettingLLMOverrideTopic,
}

// handleLLMNamespace handles /llm commands.
func (b *Bot) handleLLMNamespace(ctx context.Context, msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 {
		b.handleLLMStatus(ctx, msg)

		return
	}

	subCmd := strings.ToLower(args[0])

	switch subCmd {
	case CmdStatus:
		b.handleLLMStatus(ctx, msg)
	case subCmdSet:
		b.handleLLMSet(ctx, msg, args[1:])
	case subCmdReset:
		b.handleLLMReset(ctx, msg, args[1:])
	case "costs":
		b.handleLLMCosts(ctx, msg)
	case "budget":
		b.handleLLMBudget(ctx, msg, args[1:])
	case subCmdHelp:
		b.reply(msg, llmHelpMessage())
	default:
		b.reply(msg, llmHelpMessage())
	}
}

// handleLLMStatus displays LLM provider status and current overrides.
func (b *Bot) handleLLMStatus(ctx context.Context, msg *tgbotapi.Message) {
	statuses := b.llmClient.GetProviderStatuses()

	var sb strings.Builder

	sb.WriteString("\U0001F916 <b>LLM Provider Status</b>\n\n")
	b.writeLLMProviderStatuses(&sb, statuses)
	b.writeLLMModelOverrides(ctx, &sb)
	sb.WriteString("\n<b>Legend:</b>\n")
	sb.WriteString("\u2705 healthy | \u26A0\uFE0F circuit open | \u274C unavailable")

	b.reply(msg, sb.String())
}

// writeLLMProviderStatuses writes provider status lines to the builder.
func (b *Bot) writeLLMProviderStatuses(sb *strings.Builder, statuses []llm.ProviderStatus) {
	if len(statuses) == 0 {
		sb.WriteString("No providers configured.\n")

		return
	}

	for i, s := range statuses {
		icon := llmProviderStatusIcon(s)
		priority := llmProviderPriorityLabel(i)
		fmt.Fprintf(sb, "%s <code>%s</code>%s\n", icon, s.Name, priority)
		sb.WriteString(llmProviderStatusDetail(s))
	}
}

// llmProviderStatusIcon returns the status icon for a provider.
func llmProviderStatusIcon(s llm.ProviderStatus) string {
	if s.Available && s.CircuitBreakerOK {
		return "\u2705"
	}

	if s.Available {
		return "\u26A0\uFE0F"
	}

	return "\u274C"
}

// llmProviderPriorityLabel returns the priority label for a provider.
func llmProviderPriorityLabel(index int) string {
	if index == 0 {
		return " (primary)"
	}

	return ""
}

// llmProviderStatusDetail returns the detail line for a provider status.
func llmProviderStatusDetail(s llm.ProviderStatus) string {
	if !s.Available {
		return "   <i>not configured</i>\n"
	}

	if !s.CircuitBreakerOK {
		return "   <i>circuit breaker open</i>\n"
	}

	return ""
}

// writeLLMModelOverrides writes model override lines to the builder.
func (b *Bot) writeLLMModelOverrides(ctx context.Context, sb *strings.Builder) {
	sb.WriteString("\n<b>Model Overrides:</b>\n")

	hasOverrides := false

	for task, settingKey := range llmTaskSettings {
		var model string
		if err := b.database.GetSetting(ctx, settingKey, &model); err == nil && model != "" {
			fmt.Fprintf(sb, "\u2022 %s: <code>%s</code>\n", task, html.EscapeString(model))

			hasOverrides = true
		}
	}

	if !hasOverrides {
		sb.WriteString("<i>None (using defaults)</i>\n")
	}
}

// handleLLMSet sets a model override for a specific task.
func (b *Bot) handleLLMSet(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) < 2 {
		b.reply(msg, "Usage: <code>/llm set &lt;task&gt; &lt;model&gt;</code>\n\nTasks: summarize, cluster, narrative, topic")

		return
	}

	task := strings.ToLower(args[0])
	model := args[1]

	settingKey, ok := llmTaskSettings[task]
	if !ok {
		b.reply(msg, fmt.Sprintf("\u274C Unknown task: <code>%s</code>\n\nValid tasks: summarize, cluster, narrative, topic", html.EscapeString(task)))

		return
	}

	if err := b.database.SaveSettingWithHistory(ctx, settingKey, model, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("\u274C Failed to save: %s", err.Error()))

		return
	}

	// Refresh the override in the LLM registry so it takes effect immediately
	b.llmClient.RefreshOverride(ctx, b.database, settingKey)

	b.reply(msg, fmt.Sprintf("\u2705 Set <b>%s</b> model to <code>%s</code>", task, html.EscapeString(model)))
}

// handleLLMReset resets model override(s) to default.
func (b *Bot) handleLLMReset(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) == 0 {
		b.reply(msg, "Usage: <code>/llm reset &lt;task&gt;</code> or <code>/llm reset all</code>\n\nTasks: summarize, cluster, narrative, topic")

		return
	}

	task := strings.ToLower(args[0])

	if task == "all" {
		for taskName, settingKey := range llmTaskSettings {
			if err := b.database.DeleteSettingWithHistory(ctx, settingKey, msg.From.ID); err != nil {
				b.logger.Warn().Err(err).Str("task", taskName).Msg("failed to reset LLM override")
			}

			// Refresh the override in the LLM registry
			b.llmClient.RefreshOverride(ctx, b.database, settingKey)
		}

		b.reply(msg, "\u2705 Reset all LLM model overrides to defaults")

		return
	}

	settingKey, ok := llmTaskSettings[task]
	if !ok {
		b.reply(msg, fmt.Sprintf("\u274C Unknown task: <code>%s</code>\n\nValid tasks: summarize, cluster, narrative, topic, all", html.EscapeString(task)))

		return
	}

	if err := b.database.DeleteSettingWithHistory(ctx, settingKey, msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf("\u274C Failed to reset: %s", err.Error()))

		return
	}

	// Refresh the override in the LLM registry so it takes effect immediately
	b.llmClient.RefreshOverride(ctx, b.database, settingKey)

	b.reply(msg, fmt.Sprintf("\u2705 Reset <b>%s</b> model to default", task))
}

// handleLLMCosts displays token usage and cost tracking.
func (b *Bot) handleLLMCosts(ctx context.Context, msg *tgbotapi.Message) {
	var sb strings.Builder

	sb.WriteString("\U0001F4B0 <b>LLM Cost Tracking</b>\n\n")

	// Fetch daily usage
	dailyUsage, err := b.database.GetDailyLLMUsage(ctx)
	if err != nil {
		b.logger.Warn().Err(err).Msg("failed to fetch daily LLM usage")
	}

	// Fetch monthly usage
	monthlyUsage, err := b.database.GetMonthlyLLMUsage(ctx)
	if err != nil {
		b.logger.Warn().Err(err).Msg("failed to fetch monthly LLM usage")
	}

	// Display daily usage
	sb.WriteString("<b>Today's Usage:</b>\n")

	if dailyUsage != nil && dailyUsage.TotalRequests > 0 {
		b.writeLLMUsageSummary(&sb, dailyUsage)
	} else {
		sb.WriteString("No usage recorded today.\n")
	}

	sb.WriteString("\n<b>This Month:</b>\n")

	if monthlyUsage != nil && monthlyUsage.TotalRequests > 0 {
		b.writeLLMUsageSummary(&sb, monthlyUsage)
	} else {
		sb.WriteString("No usage recorded this month.\n")
	}

	// Add Prometheus/Grafana info
	sb.WriteString("\n<b>Real-time Metrics:</b>\n")
	sb.WriteString("View detailed metrics in Grafana dashboard.\n")
	sb.WriteString("Prometheus metrics: <code>digest_llm_*</code>")

	b.reply(msg, sb.String())
}

// writeLLMUsageSummary writes LLM usage summary to the builder.
func (b *Bot) writeLLMUsageSummary(sb *strings.Builder, usage *db.LLMUsageSummary) {
	const tokensPerK = 1000

	totalTokens := usage.TotalPromptTokens + usage.TotalCompletionTokens

	fmt.Fprintf(sb, "\u2022 Requests: <code>%d</code>\n", usage.TotalRequests)
	fmt.Fprintf(sb, "\u2022 Tokens: <code>%dk</code> (prompt: %dk, completion: %dk)\n",
		totalTokens/tokensPerK, usage.TotalPromptTokens/tokensPerK, usage.TotalCompletionTokens/tokensPerK)

	if usage.TotalCostUSD > 0 {
		fmt.Fprintf(sb, "\u2022 Est. Cost: <code>$%.4f</code>\n", usage.TotalCostUSD)
	}

	// Show by provider if multiple
	if len(usage.ByProvider) > 1 {
		sb.WriteString("\n<b>By Provider:</b>\n")

		for _, p := range usage.ByProvider {
			provTokens := p.PromptTokens + p.CompletionTokens
			fmt.Fprintf(sb, "  %s: %dk tokens, %d reqs", p.Provider, provTokens/tokensPerK, p.RequestCount)

			if p.CostUSD > 0 {
				fmt.Fprintf(sb, ", $%.4f", p.CostUSD)
			}

			sb.WriteString("\n")
		}
	}
}

// handleLLMBudget handles the /llm budget command.
func (b *Bot) handleLLMBudget(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) == 0 {
		b.showLLMBudgetStatus(msg)

		return
	}

	subCmd := strings.ToLower(args[0])

	switch subCmd {
	case subCmdSet:
		b.handleLLMBudgetSet(ctx, msg, args[1:])
	case "off", "disable":
		b.handleLLMBudgetDisable(ctx, msg)
	default:
		b.showLLMBudgetStatus(msg)
	}
}

// showLLMBudgetStatus displays current budget status.
func (b *Bot) showLLMBudgetStatus(msg *tgbotapi.Message) {
	dailyTokens, dailyLimit, percentage := b.llmClient.GetBudgetStatus()

	var sb strings.Builder

	sb.WriteString("\U0001F4CA <b>LLM Budget Status</b>\n\n")
	fmt.Fprintf(&sb, "<b>Today's Usage:</b> %s tokens\n", formatTokenCount(dailyTokens))

	if dailyLimit > 0 {
		fmt.Fprintf(&sb, "<b>Daily Limit:</b> %s tokens\n", formatTokenCount(dailyLimit))
		fmt.Fprintf(&sb, "<b>Usage:</b> %.1f%%\n", percentage*percentageMultiplier)

		if percentage >= llm.BudgetThresholdCritical {
			sb.WriteString("\n\U0001F6A8 <b>Budget exceeded!</b>")
		} else if percentage >= llm.BudgetThresholdWarning {
			sb.WriteString("\n\u26A0\uFE0F <b>Approaching budget limit</b>")
		}
	} else {
		sb.WriteString("<b>Daily Limit:</b> Not set\n")
	}

	sb.WriteString("\n\n<b>Commands:</b>\n")
	sb.WriteString("\u2022 <code>/llm budget set &lt;tokens&gt;</code> - Set daily limit\n")
	sb.WriteString("\u2022 <code>/llm budget off</code> - Disable budget alerts")

	b.reply(msg, sb.String())
}

// formatTokenCount formats a token count with K/M suffixes.
func formatTokenCount(tokens int64) string {
	const (
		thousand = 1000
		million  = 1000000
	)

	switch {
	case tokens >= million:
		return fmt.Sprintf("%.1fM", float64(tokens)/float64(million))
	case tokens >= thousand:
		return fmt.Sprintf("%.1fK", float64(tokens)/float64(thousand))
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

// handleLLMBudgetSet sets the daily token budget.
func (b *Bot) handleLLMBudgetSet(ctx context.Context, msg *tgbotapi.Message, args []string) {
	if len(args) == 0 {
		b.reply(msg, "Usage: <code>/llm budget set &lt;tokens&gt;</code>\nExample: <code>/llm budget set 500000</code>")

		return
	}

	limit, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || limit <= 0 {
		b.reply(msg, "Invalid token limit. Please provide a positive number.")

		return
	}

	// Store in settings
	if err := b.database.SaveSettingWithHistory(ctx, SettingLLMDailyBudget, args[0], msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf(errMsgFailedToSaveSetting, err))

		return
	}

	// Update runtime
	b.llmClient.SetBudgetLimit(limit)

	b.reply(msg, fmt.Sprintf("\u2705 Daily token budget set to %s tokens.\nAlerts will trigger at 80%% and 100%% usage.", formatTokenCount(limit)))
}

// handleLLMBudgetDisable disables budget alerts.
func (b *Bot) handleLLMBudgetDisable(ctx context.Context, msg *tgbotapi.Message) {
	if err := b.database.SaveSettingWithHistory(ctx, SettingLLMDailyBudget, "0", msg.From.ID); err != nil {
		b.reply(msg, fmt.Sprintf(errMsgFailedToSaveSetting, err))

		return
	}

	b.llmClient.SetBudgetLimit(0)

	b.reply(msg, "\u2705 Budget alerts disabled.")
}

func llmHelpMessage() string {
	return "\U0001F916 <b>LLM Commands</b>\n\n" +
		"<b>Status:</b>\n" +
		"\u2022 <code>/llm</code> - Show provider status and overrides\n" +
		"\u2022 <code>/llm status</code> - Show provider status and overrides\n\n" +
		"<b>Model Overrides:</b>\n" +
		"\u2022 <code>/llm set &lt;task&gt; &lt;model&gt;</code> - Set model for task\n" +
		"\u2022 <code>/llm reset &lt;task&gt;</code> - Reset to default\n" +
		"\u2022 <code>/llm reset all</code> - Reset all overrides\n\n" +
		"<b>Cost Tracking:</b>\n" +
		"\u2022 <code>/llm costs</code> - Show cost info and Prometheus metrics\n" +
		"\u2022 <code>/llm budget</code> - View daily token budget status\n" +
		"\u2022 <code>/llm budget set &lt;tokens&gt;</code> - Set daily limit\n" +
		"\u2022 <code>/llm budget off</code> - Disable budget alerts\n\n" +
		"<b>Tasks:</b> summarize, cluster, narrative, topic\n\n" +
		"<b>Example:</b>\n" +
		"<code>/llm set narrative claude-haiku-4.5</code>\n\n" +
		"<b>Current Priority:</b>\n" +
		"Google \u2192 Anthropic \u2192 OpenAI"
}
