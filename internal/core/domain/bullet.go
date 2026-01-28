package domain

import "time"

// Bullet represents an individual claim or key point extracted from a message.
// Multiple bullets can be extracted from a single Item.
type Bullet struct {
	ID              string    // Unique identifier for the bullet
	ItemID          string    // Parent item ID
	BulletIndex     int       // Index within the parent item (0-based)
	Text            string    // The bullet text content
	Topic           string    // Topic classification
	RelevanceScore  float32   // Relevance score (0-1)
	ImportanceScore float32   // Importance score (0-1)
	Embedding       []float32 // Semantic embedding vector
	BulletHash      string    // Hash for deduplication
	Status          string    // Processing status
	CreatedAt       time.Time // Creation timestamp
	TGDate          time.Time // Original message timestamp (from parent item)

	// Source information (inherited from parent item)
	SourceChannel      string
	SourceChannelTitle string
	SourceChannelID    int64
	SourceMsgID        int64
}

// Bullet status constants.
const (
	BulletStatusPending   = "pending"
	BulletStatusReady     = "ready"
	BulletStatusDuplicate = "duplicate"
	BulletStatusDropped   = "dropped"
)

// GetID returns the bullet ID.
func (b *Bullet) GetID() string {
	return b.ID
}

// GetContent returns the bullet text.
func (b *Bullet) GetContent() string {
	return b.Text
}

// GetImportanceScore returns the importance score.
func (b *Bullet) GetImportanceScore() float32 {
	return b.ImportanceScore
}

// SetImportanceScore sets the importance score.
func (b *Bullet) SetImportanceScore(score float32) {
	b.ImportanceScore = score
}

// GetRelevanceScore returns the relevance score.
func (b *Bullet) GetRelevanceScore() float32 {
	return b.RelevanceScore
}

// SetRelevanceScore sets the relevance score.
func (b *Bullet) SetRelevanceScore(score float32) {
	b.RelevanceScore = score
}

// GetTopic returns the bullet topic.
func (b *Bullet) GetTopic() string {
	return b.Topic
}

// SetTopic sets the topic.
func (b *Bullet) SetTopic(topic string) {
	b.Topic = topic
}

// GetEmbedding returns the bullet embedding.
func (b *Bullet) GetEmbedding() []float32 {
	return b.Embedding
}

// SetEmbedding sets the embedding.
func (b *Bullet) SetEmbedding(embedding []float32) {
	b.Embedding = embedding
}

// GetTimestamp returns the original message timestamp.
func (b *Bullet) GetTimestamp() time.Time {
	return b.TGDate
}

// GetSourceID returns the parent item ID.
func (b *Bullet) GetSourceID() string {
	return b.ItemID
}

// GetSourceChannelID returns the source channel.
func (b *Bullet) GetSourceChannelID() string {
	return b.SourceChannel
}

// GetSourceChannelTitle returns the source channel title.
func (b *Bullet) GetSourceChannelTitle() string {
	return b.SourceChannelTitle
}

// Ensure Bullet implements both interfaces.
var (
	_ Scorable  = (*Bullet)(nil)
	_ Groupable = (*Bullet)(nil)
)
