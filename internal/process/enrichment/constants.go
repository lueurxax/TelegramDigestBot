package enrichment

const (
	logKeyURL              = "url"
	logKeyQuery            = "query"
	fmtJSON                = "json"
	fmtErrWrapStr          = "%w: %s"
	applicationOctetStream = "application/octet-stream"
	responseTruncateLen    = 200
	unexpectedErrFmt       = "unexpected error: %v"
	errWrapFmtWithCode     = "%w: %d"
	secondsPerMinute       = 60.0
	statusError            = "error"
	statusSuccess          = "success"
)
