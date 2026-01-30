package digest

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/schedule"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// clusterGroup groups clusters or items by importance level.
type clusterGroup struct {
	clusters []db.ClusterWithItems
	items    []db.Item
}

// digestRenderContext holds all context needed for rendering a digest.
type digestRenderContext struct {
	scheduler                 *Scheduler
	llmClient                 llm.Client
	settings                  digestSettings
	items                     []db.Item
	clusters                  []db.ClusterWithItems
	start                     time.Time
	end                       time.Time
	displayStart              time.Time
	displayEnd                time.Time
	seenSummaries             map[string]bool
	factChecks                map[string]db.FactCheckMatch
	evidence                  map[string][]db.ItemEvidenceWithSource
	clusterSummaryCache       []db.ClusterSummaryCacheEntry
	clusterSummaryCacheLoaded bool
	expandLinksEnabled        bool
	expandBaseURL             string
	logger                    *zerolog.Logger
}

func (s *Scheduler) newRenderContext(ctx context.Context, settings digestSettings, items []db.Item, clusters []db.ClusterWithItems, start, end time.Time, factChecks map[string]db.FactCheckMatch, evidence map[string][]db.ItemEvidenceWithSource, logger *zerolog.Logger) *digestRenderContext {
	displayStart := start
	displayEnd := end

	if loc, ok := s.resolveScheduleLocation(ctx, logger); ok {
		displayStart = start.In(loc)
		displayEnd = end.In(loc)
	}

	// Check if expanded view links are enabled (requires signing secret and base URL)
	expandLinksEnabled := s.expandLinkGenerator != nil && s.cfg.ExpandedViewBaseURL != ""

	return &digestRenderContext{
		scheduler:          s,
		llmClient:          s.llmClient,
		settings:           settings,
		items:              items,
		clusters:           clusters,
		start:              start,
		end:                end,
		displayStart:       displayStart,
		displayEnd:         displayEnd,
		seenSummaries:      make(map[string]bool),
		factChecks:         factChecks,
		evidence:           evidence,
		expandLinksEnabled: expandLinksEnabled,
		expandBaseURL:      s.cfg.ExpandedViewBaseURL,
		logger:             logger,
	}
}

func (s *Scheduler) resolveScheduleLocation(ctx context.Context, logger *zerolog.Logger) (*time.Location, bool) {
	var sched schedule.Schedule
	if err := s.database.GetSetting(ctx, schedule.SettingDigestSchedule, &sched); err != nil {
		logger.Debug().Err(err).Msg("could not get digest_schedule for timezone")
		return nil, false
	}

	if sched.IsEmpty() {
		return nil, false
	}

	if err := sched.Validate(); err != nil {
		logger.Debug().Err(err).Msg(errInvalidScheduleTimezone)
		return nil, false
	}

	loc, err := sched.Location()
	if err != nil {
		logger.Debug().Err(err).Msg(errInvalidScheduleTimezone)
		return nil, false
	}

	return loc, true
}
