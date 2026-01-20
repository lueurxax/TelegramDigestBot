package enrichment

const (
	logKeyURL              = "url"
	logKeyQuery            = "query"
	logKeyReason           = "reason"
	logKeyItemID           = "item_id"
	logKeyDeleted          = "deleted"
	logKeyLanguage         = "language"
	fmtJSON                = "json"
	fmtErrWrapStr          = "%w: %s"
	applicationOctetStream = "application/octet-stream"
	responseTruncateLen    = 200
	unexpectedErrFmt       = "unexpected error: %v"
	errWrapFmtWithCode     = "%w: %d"
	secondsPerMinute       = 60.0
	statusError            = "error"
	statusSuccess          = "success"

	// Settings keys for domain lists
	settingEnrichmentAllowDomains = "enrichment_allow_domains"
	settingEnrichmentDenyDomains  = "enrichment_deny_domains"
)
