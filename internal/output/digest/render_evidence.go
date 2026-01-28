package digest

import (
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// appendEvidenceLine appends evidence sources to the builder.
func (rc *digestRenderContext) appendEvidenceLine(sb *strings.Builder, items []db.Item) {
	evidenceList := findEvidenceForItems(items, rc.evidence)
	if len(evidenceList) == 0 {
		return
	}

	evidenceList = filterEvidenceForDisplay(evidenceList, rc.evidenceDisplayMinAgreement())
	if len(evidenceList) == 0 {
		return
	}

	// Determine tier from evidence count and average score
	tier := determineTierFromEvidence(evidenceList)
	if tier != "" {
		sb.WriteString(formatConfidenceTier(tier, len(evidenceList)))
	}

	// Append evidence bullets (Phase 2)
	maxEvidenceBullets := 3
	for i, ev := range evidenceList {
		if i >= maxEvidenceBullets {
			break
		}

		title := ev.Source.Title
		if title == "" {
			title = ev.Source.Domain
		}

		if ev.Source.URL != "" {
			fmt.Fprintf(sb, "\n    • <a href=\"%s\">%s</a>", html.EscapeString(ev.Source.URL), html.EscapeString(title))
		} else {
			fmt.Fprintf(sb, "\n    • %s", html.EscapeString(title))
		}

		if ev.Source.Domain != "" && title != ev.Source.Domain {
			fmt.Fprintf(sb, " <i>(%s)</i>", html.EscapeString(ev.Source.Domain))
		}
	}
}

// appendExpandLink adds an expanded view link for a single item.
func (rc *digestRenderContext) appendExpandLink(sb *strings.Builder, itemID string) {
	if !rc.expandLinksEnabled || itemID == "" {
		return
	}

	token, err := rc.scheduler.expandLinkGenerator.Generate(itemID, ExpandedViewSystemUserID)
	if err != nil {
		rc.logger.Debug().Err(err).Str(logFieldItemID, itemID).Msg(logMsgExpandLinkTokenFailed)
		return
	}

	fmt.Fprintf(sb, "\n    📖 <a href=\"%s/i/%s\">More</a>", html.EscapeString(rc.expandBaseURL), token)
}

// appendExpandLinksForItems adds expand links for multiple items (used in narrative sections).
func (rc *digestRenderContext) appendExpandLinksForItems(sb *strings.Builder, items []db.Item) {
	if !rc.expandLinksEnabled || len(items) == 0 {
		return
	}

	// Limit to avoid too many links
	maxLinks := 5
	if len(items) < maxLinks {
		maxLinks = len(items)
	}

	sb.WriteString("\n    📖 ")

	for i := 0; i < maxLinks; i++ {
		if items[i].ID == "" {
			continue
		}

		token, err := rc.scheduler.expandLinkGenerator.Generate(items[i].ID, ExpandedViewSystemUserID)
		if err != nil {
			rc.logger.Debug().Err(err).Str(logFieldItemID, items[i].ID).Msg(logMsgExpandLinkTokenFailed)
			continue
		}

		if i > 0 {
			sb.WriteString(" · ")
		}

		// Use item index as label when multiple links
		fmt.Fprintf(sb, "<a href=\"%s/i/%s\">%d</a>", html.EscapeString(rc.expandBaseURL), token, i+1)
	}

	if len(items) > maxLinks {
		fmt.Fprintf(sb, " (+%d)", len(items)-maxLinks)
	}
}

// appendExplainabilityLine adds the explainability metadata line.
func (rc *digestRenderContext) appendExplainabilityLine(sb *strings.Builder, items []db.Item) {
	if !rc.settings.explainabilityLineEnabled || len(items) == 0 {
		return
	}

	var (
		maxRel float32
		maxImp float32
	)

	channelSet := make(map[string]struct{})

	for _, item := range items {
		if item.RelevanceScore > maxRel {
			maxRel = item.RelevanceScore
		}

		if item.ImportanceScore > maxImp {
			maxImp = item.ImportanceScore
		}

		if item.SourceChannel != "" {
			channelSet[item.SourceChannel] = struct{}{}
		}
	}

	chCount := len(channelSet)
	fmt.Fprintf(sb, "\n    ↳ <i>why: rel %.2f | imp %.2f | corr %dch | gate: pass</i>", maxRel, maxImp, chCount)
}

// evidenceDisplayMinAgreement returns the minimum agreement score for displaying evidence.
func (rc *digestRenderContext) evidenceDisplayMinAgreement() float32 {
	if rc == nil || rc.scheduler == nil || rc.scheduler.cfg == nil {
		return 0
	}

	minAgreement := rc.scheduler.cfg.EnrichmentMinAgreement
	if rc.scheduler.cfg.EvidenceClusteringMinScore > minAgreement {
		minAgreement = rc.scheduler.cfg.EvidenceClusteringMinScore
	}

	return minAgreement
}

// filterEvidenceForDisplay filters evidence for display based on agreement threshold.
func filterEvidenceForDisplay(evidence []db.ItemEvidenceWithSource, minAgreement float32) []db.ItemEvidenceWithSource {
	if len(evidence) == 0 {
		return nil
	}

	filtered := make([]db.ItemEvidenceWithSource, 0, len(evidence))
	seen := make(map[string]struct{}, len(evidence))

	for _, ev := range evidence {
		if ev.IsContradiction {
			continue
		}

		if ev.AgreementScore < minAgreement {
			continue
		}

		// Normalize URL to deduplicate www vs non-www variants
		key := normalizeURLForDedup(ev.Source.URL)
		if key == "" {
			key = normalizeDomain(ev.Source.Domain) + "|" + ev.Source.Title
		}

		if key != "" {
			if _, ok := seen[key]; ok {
				continue
			}

			seen[key] = struct{}{}
		}

		filtered = append(filtered, ev)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].AgreementScore > filtered[j].AgreementScore
	})

	return filtered
}

// determineTierFromEvidence determines the confidence tier based on evidence.
func determineTierFromEvidence(evidenceList []db.ItemEvidenceWithSource) string {
	if len(evidenceList) == 0 {
		return ""
	}

	var totalScore float32

	for _, ev := range evidenceList {
		totalScore += ev.AgreementScore
	}

	avgScore := totalScore / float32(len(evidenceList))

	const (
		highTierMinSources = 2
		highTierMinScore   = 0.5
		mediumTierMinScore = 0.3
	)

	if len(evidenceList) >= highTierMinSources && avgScore >= highTierMinScore {
		return db.FactCheckTierHigh
	}

	if avgScore >= mediumTierMinScore {
		return db.FactCheckTierMedium
	}

	return db.FactCheckTierLow
}

// formatConfidenceTier formats the confidence tier line.
func formatConfidenceTier(tier string, sourceCount int) string {
	if tier == "" {
		return ""
	}

	var emoji string

	switch tier {
	case db.FactCheckTierHigh:
		emoji = "✅"
	case db.FactCheckTierMedium:
		emoji = "🔵"
	case db.FactCheckTierLow:
		emoji = "⚪"
	default:
		return ""
	}

	return fmt.Sprintf("\n    ↳ <i>%s Corroborated (%d sources)</i>", emoji, sourceCount)
}

// findEvidenceForItems finds evidence for a list of items.
func findEvidenceForItems(items []db.Item, evidence map[string][]db.ItemEvidenceWithSource) []db.ItemEvidenceWithSource {
	if evidence == nil {
		return nil
	}

	for _, item := range items {
		if item.ID == "" {
			continue
		}

		if ev, ok := evidence[item.ID]; ok && len(ev) > 0 {
			return ev
		}
	}

	return nil
}

// convertEvidenceForLLM converts database evidence to LLM-compatible format.
func (rc *digestRenderContext) convertEvidenceForLLM(items []db.Item) llm.ItemEvidence {
	result := make(llm.ItemEvidence)
	minAgreement := rc.evidenceDisplayMinAgreement()

	for _, item := range items {
		if ev, ok := rc.evidence[item.ID]; ok && len(ev) > 0 {
			filtered := filterEvidenceForDisplay(ev, minAgreement)
			if len(filtered) == 0 {
				continue
			}

			sources := make([]llm.EvidenceSource, 0, len(filtered))

			for _, e := range filtered {
				sources = append(sources, llm.EvidenceSource{
					URL:             e.Source.URL,
					Domain:          e.Source.Domain,
					Title:           e.Source.Title,
					AgreementScore:  e.AgreementScore,
					IsContradiction: e.IsContradiction,
				})
			}

			result[item.ID] = sources
		}
	}

	return result
}
