package bot

// helpSummaryMessage returns the main help summary message.
func helpSummaryMessage() string {
	return "\U0001F44B <b>Telegram Digest Bot</b>\n\n" +
		"Quick start:\n" +
		"\u2022 <code>/setup</code> - Guided setup\n" +
		"\u2022 <code>/status</code> - System status\n" +
		"\u2022 <code>/preview</code> - Preview next digest\n\n" +
		"Core areas:\n" +
		"\u2022 <code>/channel</code> - Manage sources\n" +
		"\u2022 <code>/filter</code> - Filter rules\n" +
		"\u2022 <code>/discover</code> - Channel discovery\n" +
		"\u2022 <code>/schedule</code> - Digest timing\n" +
		"\u2022 <code>/config</code> - Settings\n" +
		"\u2022 <code>/ai</code> - AI features\n" +
		"\u2022 <code>/system</code> - Diagnostics\n" +
		"\u2022 <code>/research</code> - Research dashboard\n\n" +
		"Data & feedback:\n" +
		"\u2022 <code>/scores</code> <code>/factcheck</code> <code>/enrichment</code> <code>/ratings</code> <code>/annotate</code> <code>/feedback</code>\n\n" +
		"More: <code>/help &lt;topic&gt;</code> (channels, discover, filters, schedule, config, ai, enrichment, system, research, scores, factcheck, ratings, annotate)\n" +
		"Full list: <code>/help all</code>\n" +
		"BotFather list: <code>/help botfather</code>"
}

// helpChannelsMessage returns the help message for channel commands.
func helpChannelsMessage() string {
	return "\U0001F4CB <b>Channel Management</b>\n" +
		"\u2022 <code>/channel add &lt;id|@user|link&gt;</code>\n" +
		"\u2022 <code>/channel remove &lt;id|@user&gt;</code>\n" +
		"\u2022 <code>/channel list</code>\n" +
		"\u2022 <code>/channel weight &lt;@user&gt; [0.1-2.0|auto]</code>\n" +
		"\u2022 <code>/channel relevance &lt;@user&gt; [auto|manual]</code>\n" +
		"\u2022 <code>/channel stats</code>"
}

// helpFiltersMessage returns the help message for filter commands.
func helpFiltersMessage() string {
	return "\U0001F50D <b>Filters</b>\n" +
		"\u2022 <code>/filter list</code>\n" +
		"\u2022 <code>/filter add &lt;allow|deny&gt; &lt;pattern&gt;</code>\n" +
		"\u2022 <code>/filter remove &lt;pattern&gt;</code>\n" +
		"\u2022 <code>/filter ads &lt;on|off&gt;</code>\n" +
		"\u2022 <code>/filter mode &lt;mixed|allow|deny&gt;</code>\n" +
		"\u2022 <code>/filter keywords</code>\n" +
		"\u2022 <code>/filter min_length &lt;n&gt;</code>\n" +
		"\u2022 <code>/filter skip_forwards &lt;on|off&gt;</code>"
}

// helpDiscoverMessage returns the help message for discovery commands.
func helpDiscoverMessage() string {
	return "\U0001F9ED <b>Discovery</b>\n" +
		"\u2022 <code>/discover</code> - Show actionable discoveries\n" +
		"\u2022 <code>/discover preview &lt;@username&gt;</code> - Explain why it is (not) actionable\n" +
		"\u2022 <code>/discover approve &lt;@username&gt;</code>\n" +
		"\u2022 <code>/discover reject &lt;@username&gt;</code>\n" +
		"\u2022 <code>/discover allow [add|remove|clear] &lt;word&gt;</code>\n" +
		"\u2022 <code>/discover deny [add|remove|clear] &lt;word&gt;</code>\n" +
		"\u2022 <code>/discover min_seen &lt;n&gt;</code> - Min count for discovery\n" +
		"\u2022 <code>/discover min_engagement &lt;n&gt;</code> - Min engagement score\n" +
		"\u2022 <code>/discover show-rejected [limit]</code>\n" +
		"\u2022 <code>/discover cleanup</code>\n" +
		"\u2022 <code>/discover stats</code>"
}

// helpScheduleMessage returns the help message for schedule commands.
func helpScheduleMessage() string {
	return "\U0001F5D3\uFE0F <b>Schedule</b>\n" +
		"Times are hour-only (<code>HH:00</code>).\n" +
		"\u2022 <code>/schedule timezone &lt;IANA&gt;</code>\n" +
		"\u2022 <code>/schedule weekdays times &lt;HH:00,...&gt;</code>\n" +
		"\u2022 <code>/schedule weekdays hourly &lt;HH:00-HH:00&gt;</code>\n" +
		"\u2022 <code>/schedule weekends hourly &lt;HH:00-HH:00&gt;</code>\n" +
		"\u2022 <code>/schedule preview [count]</code>\n" +
		"\u2022 <code>/schedule clear</code>\n" +
		"\u2022 <code>/schedule show</code>"
}

// helpConfigMessage returns the help message for configuration commands.
func helpConfigMessage() string {
	return "\u2699\uFE0F <b>Configuration</b>\n" +
		"\u2022 <code>/config target &lt;id|@user&gt;</code>\n" +
		"\u2022 <code>/config window &lt;duration&gt;</code>\n" +
		"\u2022 <code>/config language &lt;code&gt;</code>\n" +
		"\u2022 <code>/config tone &lt;professional|casual|brief&gt;</code>\n" +
		"\u2022 <code>/config relevance &lt;0-1&gt;</code>\n" +
		"\u2022 <code>/config importance &lt;0-1&gt;</code>\n" +
		"\u2022 <code>/config links &lt;on|off&gt;</code>\n" +
		"\u2022 <code>/config maxlinks &lt;n&gt;</code>\n" +
		"\u2022 <code>/config discovery_min_seen &lt;n&gt;</code>\n" +
		"\u2022 <code>/config discovery_min_engagement &lt;n&gt;</code>\n" +
		"\u2022 <code>/config reset &lt;key&gt;</code>"
}

// helpAIMessage returns the help message for AI-related commands.
func helpAIMessage() string {
	return "\U0001F9E0 <b>AI &amp; Features</b>\n" +
		"\u2022 <code>/ai tone &lt;professional|casual|brief&gt;</code>\n" +
		"\u2022 <code>/ai prompt</code>\n" +
		"\u2022 <code>/ai editor &lt;on|off&gt;</code>\n" +
		"\u2022 <code>/ai tiered &lt;on|off&gt;</code>\n" +
		"\u2022 <code>/ai vision &lt;on|off&gt;</code>\n" +
		"\u2022 <code>/ai consolidated &lt;on|off&gt;</code>\n" +
		"\u2022 <code>/ai details &lt;on|off&gt;</code>\n" +
		"\u2022 <code>/ai topics &lt;on|off&gt;</code>\n" +
		"\u2022 <code>/ai dedup &lt;mode&gt;</code>"
}

// helpSystemMessage returns the help message for system commands.
func helpSystemMessage() string {
	return "\U0001F6E0 <b>System</b>\n" +
		"\u2022 <code>/system status</code>\n" +
		"\u2022 <code>/system settings</code>\n" +
		"\u2022 <code>/system errors</code>\n" +
		"\u2022 <code>/system retry</code>\n" +
		"\u2022 <code>/system factcheck</code>"
}

// helpScoresMessage returns the help message for scores commands.
func helpScoresMessage() string {
	return "\U0001F4CA <b>Scores</b>\n" +
		"\u2022 <code>/scores [hours] [limit]</code>\n" +
		"\u2022 <code>/scores debug [hours]</code>\n" +
		"\u2022 <code>/scores debug reasons [hours]</code>"
}

// helpFactCheckMessage returns the help message for fact check commands.
func helpFactCheckMessage() string {
	return "\U0001F50E <b>Fact Check</b>\n" +
		"\u2022 <code>/factcheck [limit]</code> - Queue, cache, and match stats"
}

// helpRatingsMessage returns the help message for ratings commands.
func helpRatingsMessage() string {
	return "\u2B50 <b>Ratings</b>\n" +
		"\u2022 <code>/ratings [days] [limit]</code>\n" +
		"\u2022 <code>/ratings stats [limit]</code>"
}

// helpAnnotateMessage returns the help message for annotation commands.
func helpAnnotateMessage() string {
	return "\U0001F9E9 <b>Annotations</b>\n" +
		"\u2022 <code>/annotate</code> - enqueue/next/label/skip/stats"
}

// helpResearchMessage returns the help message for research commands.
func helpResearchMessage() string {
	return "\U0001F50E <b>Research Dashboard</b>\n" +
		"\u2022 <code>/research login</code> - generate a login link for the research UI\n" +
		"\u2022 <code>/research rebuild</code> - refresh research materialized views"
}

// helpAllMessage returns the combined help message for all commands.
func helpAllMessage() string {
	return helpSummaryMessage() + "\n\n" +
		helpChannelsMessage() + "\n\n" +
		helpDiscoverMessage() + "\n\n" +
		helpFiltersMessage() + "\n\n" +
		helpScheduleMessage() + "\n\n" +
		helpConfigMessage() + "\n\n" +
		helpAIMessage() + "\n\n" +
		enrichmentHelpMessage() + "\n\n" +
		helpSystemMessage() + "\n\n" +
		helpResearchMessage() + "\n\n" +
		helpScoresMessage() + "\n\n" +
		helpFactCheckMessage() + "\n\n" +
		helpRatingsMessage() + "\n\n" +
		helpAnnotateMessage()
}

// botFatherCommandsMessage returns the message for BotFather commands setup.
func botFatherCommandsMessage() string {
	return "Use <code>/setcommands</code> in BotFather with:\n\n" +
		"<code>" +
		"start - Show help\n" +
		"help - Command overview\n" +
		"setup - Guided setup\n" +
		"status - System status\n" +
		"preview - Preview next digest\n" +
		"channel - Manage channels\n" +
		"filter - Manage filters\n" +
		"config - Configure settings\n" +
		"schedule - Digest schedule\n" +
		"ai - AI features\n" +
		"system - System tools\n" +
		"research - Research dashboard\n" +
		"scores - Score stats\n" +
		"factcheck - Fact check status\n" +
		"ratings - Rating stats\n" +
		"annotate - Annotation queue\n" +
		"discover - Channel discovery\n" +
		"feedback - Rate an item\n" +
		"settings - Show current settings" +
		"</code>"
}
