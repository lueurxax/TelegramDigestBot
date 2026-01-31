package bot

import (
	"context"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// commandHandler is a function that handles a specific bot command.
type commandHandler func(ctx context.Context, msg *tgbotapi.Message)

// commandRegistry holds the mapping of command names to their handlers.
type commandRegistry struct {
	handlers       map[string]commandHandler
	toggleSettings map[string]string // command -> setting key for toggle commands
}

// newCommandRegistry creates a new command registry for the bot.
func (b *Bot) newCommandRegistry() *commandRegistry {
	r := &commandRegistry{
		handlers:       make(map[string]commandHandler),
		toggleSettings: make(map[string]string),
	}

	b.registerCoreCommands(r)
	b.registerSettingCommands(r)
	b.registerToggleCommands(r)

	return r
}

func (b *Bot) registerCoreCommands(r *commandRegistry) {
	// Basic commands
	r.handlers["start"] = b.handleHelp
	r.handlers["help"] = b.handleHelp
	r.handlers["botfather"] = b.handleBotFather
	r.handlers["commands"] = b.handleBotFather
	r.handlers["setup"] = b.handleSetup
	r.handlers[CmdStatus] = b.handleStatus
	r.handlers["preview"] = b.handlePreview
	r.handlers[CmdResearch] = b.handleResearch

	// Namespace commands
	r.handlers[CmdChannel] = b.handleChannelNamespace
	r.handlers["filter"] = b.handleFilterNamespace
	r.handlers["config"] = b.handleConfigNamespace
	r.handlers["ai"] = b.handleAINamespace
	r.handlers["system"] = b.handleSystemNamespace
	r.handlers["discover"] = b.handleDiscoverNamespace
	r.handlers[CmdLLM] = b.handleLLMNamespace
}

func (b *Bot) registerSettingCommands(r *commandRegistry) {
	r.handlers[CmdSettings] = b.handleSettings
	r.handlers[CmdHistory] = b.handleHistory
	r.handlers[CmdAdd] = b.handleAddChannel
	r.handlers[CmdList] = b.handleListChannels
	r.handlers[CmdRemove] = b.handleRemoveChannel
	r.handlers["feedback"] = b.handleFeedback
	r.handlers[CmdRatings] = b.handleRatings
	r.handlers[CmdScores] = b.handleScores
	r.handlers[CmdFactCheck] = b.handleFactCheck
	r.handlers[CmdEnrichment] = b.handleEnrichmentNamespace
	r.handlers[CmdPrompt] = b.handlePrompt
	r.handlers["filters"] = b.handleFilters
	r.handlers[CmdTarget] = b.handleTarget
	r.handlers[CmdWindow] = b.handleWindow
	r.handlers[CmdSchedule] = b.handleSchedule
	r.handlers[CmdTopics] = b.handleTopics
	r.handlers[CmdDedup] = b.handleDedup
	r.handlers[CmdLanguage] = b.handleLanguage
	r.handlers[CmdTone] = b.handleTone
	r.handlers[CmdErrors] = b.handleErrors
	r.handlers[CmdRetry] = b.handleRetry

	// Threshold commands
	r.handlers[CmdRelevance] = func(ctx context.Context, msg *tgbotapi.Message) {
		b.handleThreshold(ctx, msg, SettingRelevanceThreshold)
	}
	r.handlers[CmdImportance] = func(ctx context.Context, msg *tgbotapi.Message) {
		b.handleThreshold(ctx, msg, SettingImportanceThreshold)
	}

	// Commands with aliases
	r.handlers[CmdMinLength] = b.handleMinLength
	r.handlers[CmdMinLengthAlt] = b.handleMinLength
	r.handlers["ads_keywords"] = b.handleAdsKeywords
	r.handlers["adskeywords"] = b.handleAdsKeywords
}

func (b *Bot) registerToggleCommands(r *commandRegistry) {
	r.toggleSettings[CmdSkipForwards] = SettingFiltersSkipForwards
	r.toggleSettings[CmdSkipFwdAlt] = SettingFiltersSkipForwards
	r.toggleSettings[CmdEditor] = SettingEditorEnabled
	r.toggleSettings[CmdTiered] = SettingTieredImportanceEnabled
	r.toggleSettings[CmdVision] = SettingVisionRoutingEnabled
	r.toggleSettings["vision_routing"] = SettingVisionRoutingEnabled
	r.toggleSettings[CmdVisionAlt] = SettingVisionRoutingEnabled
	r.toggleSettings[CmdConsolidated] = SettingConsolidatedClustersEnabled
	r.toggleSettings["editor_details"] = SettingEditorDetailedItems
	r.toggleSettings[CmdEditorDetail] = SettingEditorDetailedItems
	r.toggleSettings[CmdCoverImage] = SettingDigestCoverImage
	r.toggleSettings[CmdCoverImageAlt] = SettingDigestCoverImage
	r.toggleSettings[CmdAICover] = SettingDigestAICover
	r.toggleSettings[CmdAICoverAlt] = SettingDigestAICover
	r.toggleSettings[CmdInlineImages] = SettingDigestInlineImages
	r.toggleSettings[CmdInlineImagesAlt] = SettingDigestInlineImages
	r.toggleSettings[CmdOthersNarrative] = SettingOthersAsNarrative
	r.toggleSettings[CmdOthersNarrativeAlt] = SettingOthersAsNarrative
}

// route handles the command routing for a message.
func (r *commandRegistry) route(ctx context.Context, b *Bot, msg *tgbotapi.Message) bool {
	cmd := msg.Command()

	if settingKey, ok := r.toggleSettings[cmd]; ok {
		b.handleToggleSetting(ctx, msg, settingKey)

		return true
	}

	if handler, ok := r.handlers[cmd]; ok {
		handler(ctx, msg)

		return true
	}

	return false
}

// prepareSubcommandMessage creates a modified message for namespace subcommand routing.
func prepareSubcommandMessage(msg *tgbotapi.Message, subcommand string, args []string) tgbotapi.Message {
	newMsg := *msg
	newMsg.Text = "/" + subcommand

	if len(args) > 1 {
		newMsg.Text += " " + strings.Join(args[1:], " ")
	}

	newEntities := make([]tgbotapi.MessageEntity, len(msg.Entities))
	copy(newEntities, msg.Entities)

	for i := range newEntities {
		if newEntities[i].Type == EntityTypeBotCommand && newEntities[i].Offset == 0 {
			newEntities[i].Length = len(subcommand) + 1
		}
	}

	newMsg.Entities = newEntities

	return newMsg
}
