package domain

import "time"

// Scorable defines the interface for entities that can be scored and deduplicated.
// This abstraction allows both Items and Bullets to be processed uniformly
// in deduplication, clustering, and scoring pipelines.
type Scorable interface {
	// GetID returns the unique identifier for this scorable entity.
	GetID() string

	// GetContent returns the text content for similarity comparison.
	GetContent() string

	// GetImportanceScore returns the importance score (0-1).
	GetImportanceScore() float32

	// SetImportanceScore sets the importance score.
	SetImportanceScore(score float32)

	// GetRelevanceScore returns the relevance score (0-1).
	GetRelevanceScore() float32

	// SetRelevanceScore sets the relevance score.
	SetRelevanceScore(score float32)

	// GetTopic returns the topic classification.
	GetTopic() string

	// SetTopic sets the topic classification.
	SetTopic(topic string)

	// GetEmbedding returns the embedding vector for semantic operations.
	GetEmbedding() []float32

	// SetEmbedding sets the embedding vector.
	SetEmbedding(embedding []float32)

	// GetTimestamp returns the timestamp for time-based operations.
	GetTimestamp() time.Time

	// GetSourceID returns the ID of the source entity (parent item for bullets).
	GetSourceID() string
}

// Groupable extends Scorable with grouping capabilities.
// Used for entities that can be grouped into clusters.
type Groupable interface {
	Scorable

	// GetSourceChannelID returns the channel identifier for corroboration.
	GetSourceChannelID() string

	// GetSourceChannelTitle returns the channel title for display.
	GetSourceChannelTitle() string
}

// ScorableAdapter wraps Item to implement Scorable interface.
// This allows existing Items to work with the new abstraction without modification.
type ScorableAdapter struct {
	item *Item
}

// NewScorableAdapter creates a new ScorableAdapter for an Item.
func NewScorableAdapter(item *Item) *ScorableAdapter {
	return &ScorableAdapter{item: item}
}

// GetID returns the item ID.
func (a *ScorableAdapter) GetID() string {
	return a.item.ID
}

// GetContent returns the item summary.
func (a *ScorableAdapter) GetContent() string {
	return a.item.Summary
}

// GetImportanceScore returns the importance score.
func (a *ScorableAdapter) GetImportanceScore() float32 {
	return a.item.ImportanceScore
}

// SetImportanceScore sets the importance score.
func (a *ScorableAdapter) SetImportanceScore(score float32) {
	a.item.ImportanceScore = score
}

// GetRelevanceScore returns the relevance score.
func (a *ScorableAdapter) GetRelevanceScore() float32 {
	return a.item.RelevanceScore
}

// SetRelevanceScore sets the relevance score.
func (a *ScorableAdapter) SetRelevanceScore(score float32) {
	a.item.RelevanceScore = score
}

// GetTopic returns the item topic.
func (a *ScorableAdapter) GetTopic() string {
	return a.item.Topic
}

// SetTopic sets the topic.
func (a *ScorableAdapter) SetTopic(topic string) {
	a.item.Topic = topic
}

// GetEmbedding returns the item embedding.
func (a *ScorableAdapter) GetEmbedding() []float32 {
	return a.item.Embedding
}

// SetEmbedding sets the embedding.
func (a *ScorableAdapter) SetEmbedding(embedding []float32) {
	a.item.Embedding = embedding
}

// GetTimestamp returns the Telegram date.
func (a *ScorableAdapter) GetTimestamp() time.Time {
	return a.item.TGDate
}

// GetSourceID returns the raw message ID.
func (a *ScorableAdapter) GetSourceID() string {
	return a.item.RawMessageID
}

// GetSourceChannelID returns the source channel.
func (a *ScorableAdapter) GetSourceChannelID() string {
	return a.item.SourceChannel
}

// GetSourceChannelTitle returns the source channel title.
func (a *ScorableAdapter) GetSourceChannelTitle() string {
	return a.item.SourceChannelTitle
}

// Item returns the underlying Item pointer.
func (a *ScorableAdapter) Item() *Item {
	return a.item
}

// Ensure ScorableAdapter implements both interfaces.
var (
	_ Scorable  = (*ScorableAdapter)(nil)
	_ Groupable = (*ScorableAdapter)(nil)
)
