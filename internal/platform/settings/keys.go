package settings

import "time"

const SettingLinkBackfillRequest = "link_backfill_request"

const DefaultLinkBackfillLimit = 500

type LinkBackfillRequest struct {
	RequestedAt time.Time `json:"requested_at"`
	RequestedBy int64     `json:"requested_by"`
	Hours       int       `json:"hours"`
	Limit       int       `json:"limit"`
}
