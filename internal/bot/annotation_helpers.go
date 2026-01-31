package bot

const (
	annotateBlockquoteFmt = "<blockquote>%s</blockquote>\n"
	annotateUnknown       = "unknown"
	fmtItemCode           = "Item: <code>%s</code>\n"
	fmtStatusCode         = "Status: <code>%s</code>\n"
	fmtScoresCode         = "Scores: rel <code>%.2f</code> | imp <code>%.2f</code>\n"
	fmtTopicCode          = "Topic: <code>%s</code>\n"
	fmtTimeCode           = "Time: <code>%s</code>\n"
	fmtSummaryHdr         = "\nSummary:\n"
	fmtTextHdr            = "\nText:\n"
	fmtOpenMessage        = "Open message"
)

func truncateAnnotationText(text string, limit int) string {
	if limit <= 0 {
		return "..."
	}

	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}

	return string(runes[:limit]) + "..."
}
