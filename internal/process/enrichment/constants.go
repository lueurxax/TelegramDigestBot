package enrichment

const (
	logKeyURL               = "url"
	logKeyQuery             = "query"
	logKeyReason            = "reason"
	logKeyItemID            = "item_id"
	logKeyDeleted           = "deleted"
	logKeyLanguage          = "language"
	logKeySourceLang        = "source_lang"
	logKeyTargetLang        = "target_lang"
	logKeyItemLang          = "item_lang"
	logKeyEvidenceLang      = "evidence_lang"
	fmtJSON                 = "json"
	fmtErrWrapStr           = "%w: %s"
	applicationOctetStream  = "application/octet-stream"
	responseTruncateLen     = 200
	unexpectedErrFmt        = "unexpected error: %v"
	errWrapFmtWithCode      = "%w: %d"
	fmtErrTranslateTo       = "translate text to %s: %w"
	fmtErrTranslateForScore = "translate summary for scoring to %s: %w"
	secondsPerMinute        = 60.0
	statusError             = "error"
	statusSuccess           = "success"

	// Settings keys for domain lists
	settingEnrichmentAllowDomains   = "enrichment_allow_domains"
	settingEnrichmentDenyDomains    = "enrichment_deny_domains"
	settingEnrichmentLanguagePolicy = "enrichment_language_policy"
)
