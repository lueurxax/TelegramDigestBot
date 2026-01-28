package digest

import (
	"context"

	"github.com/rs/zerolog"
)

// digestSettings holds all settings needed for building a digest.
type digestSettings struct {
	topicsEnabled               bool
	freshnessDecayHours         int
	freshnessFloor              float32
	topicDiversityCap           float32
	minTopicCount               int
	editorEnabled               bool
	consolidatedClustersEnabled bool
	editorDetailedItems         bool
	digestLanguage              string
	digestTone                  string
	othersAsNarrative           bool
	corroborationBoost          float32
	singleSourcePenalty         float32
	explainabilityLineEnabled   bool
}

const errInvalidScheduleTimezone = "invalid digest schedule timezone"

func (s *Scheduler) getDigestSettings(ctx context.Context, logger *zerolog.Logger) digestSettings {
	ds := digestSettings{
		topicsEnabled:             true,
		freshnessDecayHours:       s.cfg.FreshnessDecayHours,
		freshnessFloor:            s.cfg.FreshnessFloor,
		topicDiversityCap:         s.cfg.TopicDiversityCap,
		minTopicCount:             s.cfg.MinTopicCount,
		editorDetailedItems:       true,
		corroborationBoost:        s.cfg.CorroborationImportanceBoost,
		singleSourcePenalty:       s.cfg.SingleSourcePenalty,
		explainabilityLineEnabled: s.cfg.ExplainabilityLineEnabled,
	}

	s.loadDigestSettingsFromDB(ctx, logger, &ds)

	return ds
}

func (s *Scheduler) loadDigestSettingsFromDB(ctx context.Context, logger *zerolog.Logger, ds *digestSettings) {
	loadSetting := func(key string, target interface{}, logMsg string) {
		if err := s.database.GetSetting(ctx, key, target); err != nil {
			logger.Debug().Err(err).Msg(logMsg)
		}
	}

	loadSetting("topics_enabled", &ds.topicsEnabled, "could not get topics_enabled from DB")
	loadSetting("freshness_decay_hours", &ds.freshnessDecayHours, "could not get freshness_decay_hours from DB")
	loadSetting("freshness_floor", &ds.freshnessFloor, "could not get freshness_floor from DB")
	loadSetting("topic_diversity_cap", &ds.topicDiversityCap, "could not get topic_diversity_cap from DB")
	loadSetting("min_topic_count", &ds.minTopicCount, "could not get min_topic_count from DB")
	loadSetting("editor_enabled", &ds.editorEnabled, "could not get editor_enabled from DB")
	loadSetting("consolidated_clusters_enabled", &ds.consolidatedClustersEnabled, "could not get consolidated_clusters_enabled from DB")
	loadSetting("editor_detailed_items", &ds.editorDetailedItems, "could not get editor_detailed_items from DB")
	loadSetting(SettingDigestLanguage, &ds.digestLanguage, MsgCouldNotGetDigestLanguage)
	loadSetting("digest_tone", &ds.digestTone, "could not get digest_tone from DB")
	loadSetting("others_as_narrative", &ds.othersAsNarrative, "could not get others_as_narrative from DB")
}
