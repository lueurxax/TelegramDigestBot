package enrichment

const (
	testURL1            = "https://example.com/1"
	expected1ResultGot  = "expected 1 result, got %d"
	expectedURLFmt      = "expected URL https://example.com/1, got %s"
	expectedErrGotNil   = "expected error, got nil"
	expected0ResultsGot = "expected 0 results, got %d"
	failedToWriteResp   = "failed to write response: %v"
	expectedFmt         = "expected %q, got %q"
	testQueryFull       = "test query"

	newsAPIEmptyResponse = `{
			"status": "ok",
			"totalResults": 0,
			"articles": []
		}`
)
