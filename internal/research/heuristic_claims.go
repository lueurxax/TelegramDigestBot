package research

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/embeddings"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	defaultHeuristicBatchSize    = 500
	maxHeuristicBatchSize        = 2000
	embeddingSimilarityThreshold = 0.85
	maxSimilarClaimsToCheck      = 10
	claimTextPreviewLength       = 50
	logFieldClaimID              = "logFieldClaimID"
)

// HeuristicClaimPopulator coordinates heuristic claim extraction.
type HeuristicClaimPopulator struct {
	db              HeuristicClaimRepository
	extractor       *ClaimExtractor
	embeddingClient embeddings.Client
	logger          *zerolog.Logger
	batchSize       int
}

// HeuristicClaimRepository defines the database interface for heuristic claims.
type HeuristicClaimRepository interface {
	GetItemsWithoutEvidenceClaims(ctx context.Context, limit int) ([]db.ItemForHeuristicClaim, error)
	InsertHeuristicClaims(ctx context.Context, claims []db.HeuristicClaimInput) (int64, error)
	FindSimilarClaimsByEmbedding(ctx context.Context, embedding []float32, limit int, threshold float64) ([]db.SimilarClaim, error)
	UpdateClaimClusters(ctx context.Context, claimID string, clusterIDs []string) error
}

// NewHeuristicClaimPopulator creates a new populator.
func NewHeuristicClaimPopulator(database HeuristicClaimRepository, logger *zerolog.Logger) *HeuristicClaimPopulator {
	return &HeuristicClaimPopulator{
		db:        database,
		extractor: NewClaimExtractor(),
		logger:    logger,
		batchSize: defaultHeuristicBatchSize,
	}
}

// SetEmbeddingClient sets the embedding client for semantic deduplication.
func (p *HeuristicClaimPopulator) SetEmbeddingClient(client embeddings.Client) {
	p.embeddingClient = client
}

// SetBatchSize sets the batch size for processing items.
func (p *HeuristicClaimPopulator) SetBatchSize(size int) {
	if size > 0 && size <= maxHeuristicBatchSize {
		p.batchSize = size
	}
}

// PopulateHeuristicClaims extracts and inserts heuristic claims for items without evidence.
func (p *HeuristicClaimPopulator) PopulateHeuristicClaims(ctx context.Context) (int64, error) {
	items, err := p.db.GetItemsWithoutEvidenceClaims(ctx, p.batchSize)
	if err != nil {
		return 0, fmt.Errorf("get items without evidence: %w", err)
	}

	if len(items) == 0 {
		p.logger.Debug().Msg("no items without evidence claims found")
		return 0, nil
	}

	p.logger.Info().Int(scopeItems, len(items)).Msg("extracting heuristic claims")

	clusterClaims := p.extractClaimsFromItems(items)
	claims := p.buildClaimInputs(ctx, items, clusterClaims)

	if len(claims) == 0 {
		p.logger.Debug().Msg("no heuristic claims extracted")
		return 0, nil
	}

	inserted, err := p.db.InsertHeuristicClaims(ctx, claims)
	if err != nil {
		return 0, fmt.Errorf("insert heuristic claims: %w", err)
	}

	p.logger.Info().
		Int64("inserted", inserted).
		Int("total_claims", len(claims)).
		Int("items_processed", len(items)).
		Msg("heuristic claims populated")

	return inserted, nil
}

// extractClaimsFromItems extracts claims from items and groups by cluster.
func (p *HeuristicClaimPopulator) extractClaimsFromItems(items []db.ItemForHeuristicClaim) map[string][]HeuristicClaim {
	clusterClaims := make(map[string][]HeuristicClaim)

	for _, item := range items {
		extracted := p.extractor.ExtractClaims(item.Summary)
		clusterClaims[item.ClusterID] = append(clusterClaims[item.ClusterID], extracted...)
	}

	return clusterClaims
}

// buildClaimInputs deduplicates claims and builds database input structs.
// It uses normalized hash for exact dedup and optionally embedding similarity for semantic dedup.
func (p *HeuristicClaimPopulator) buildClaimInputs(ctx context.Context, items []db.ItemForHeuristicClaim, clusterClaims map[string][]HeuristicClaim) []db.HeuristicClaimInput {
	var claims []db.HeuristicClaimInput

	seenHashes := make(map[string]bool)
	itemsByCluster := buildItemsByClusterMap(items)

	var mergedCount int

	for clusterID, clusterClaimList := range clusterClaims {
		firstItem := itemsByCluster[clusterID]
		if firstItem == nil {
			continue
		}

		for _, claim := range clusterClaimList {
			if seenHashes[claim.NormalizedHash] {
				updateExistingClaim(claims, claim.NormalizedHash, clusterID)

				continue
			}

			// Check for semantic duplicates using embeddings
			if p.embeddingClient != nil {
				merged := p.checkAndMergeSimilarClaim(ctx, claim.Text, clusterID)
				if merged {
					mergedCount++

					continue
				}
			}

			seenHashes[claim.NormalizedHash] = true

			input := db.HeuristicClaimInput{
				ClaimText:       claim.Text,
				NormalizedHash:  claim.NormalizedHash,
				FirstSeenAt:     firstItem.TgDate,
				OriginClusterID: clusterID,
				ClusterIDs:      []string{clusterID},
			}

			// Generate embedding for the new claim
			if p.embeddingClient != nil {
				embedding, err := p.embeddingClient.GetEmbedding(ctx, claim.Text)
				if err != nil {
					p.logger.Debug().Err(err).Str("claim", claim.Text[:min(claimTextPreviewLength, len(claim.Text))]).Msg("failed to generate embedding")
				} else {
					input.Embedding = embedding
				}
			}

			claims = append(claims, input)
		}
	}

	if mergedCount > 0 {
		p.logger.Info().Int("merged_claims", mergedCount).Msg("merged semantically similar claims")
	}

	return claims
}

// checkAndMergeSimilarClaim checks if a claim is semantically similar to existing claims.
// If found, it merges the cluster ID with the existing claim and returns true.
func (p *HeuristicClaimPopulator) checkAndMergeSimilarClaim(ctx context.Context, claimText, clusterID string) bool {
	embedding, err := p.embeddingClient.GetEmbedding(ctx, claimText)
	if err != nil {
		p.logger.Debug().Err(err).Msg("failed to generate embedding for similarity check")
		return false
	}

	similar, err := p.db.FindSimilarClaimsByEmbedding(ctx, embedding, maxSimilarClaimsToCheck, embeddingSimilarityThreshold)
	if err != nil {
		p.logger.Debug().Err(err).Msg("failed to find similar claims")
		return false
	}

	if len(similar) == 0 {
		return false
	}

	// Merge with the most similar claim
	bestMatch := similar[0]

	err = p.db.UpdateClaimClusters(ctx, bestMatch.ID, []string{clusterID})
	if err != nil {
		p.logger.Debug().Err(err).Str(logFieldClaimID, bestMatch.ID).Msg("failed to update claim clusters")
		return false
	}

	p.logger.Debug().
		Str(logFieldClaimID, bestMatch.ID).
		Float64("similarity", bestMatch.Similarity).
		Str("cluster_id", clusterID).
		Msg("merged claim with existing similar claim")

	return true
}

// buildItemsByClusterMap creates a map from cluster ID to first item.
func buildItemsByClusterMap(items []db.ItemForHeuristicClaim) map[string]*db.ItemForHeuristicClaim {
	result := make(map[string]*db.ItemForHeuristicClaim)

	for i := range items {
		item := &items[i]
		if _, exists := result[item.ClusterID]; !exists {
			result[item.ClusterID] = item
		}
	}

	return result
}

// updateExistingClaim adds a cluster ID to an existing claim.
func updateExistingClaim(claims []db.HeuristicClaimInput, hash, clusterID string) {
	for i := range claims {
		if claims[i].NormalizedHash == hash {
			claims[i].ClusterIDs = appendUnique(claims[i].ClusterIDs, clusterID)

			return
		}
	}
}

// appendUnique appends a string to a slice if not already present.
func appendUnique(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}

	return append(slice, s)
}
