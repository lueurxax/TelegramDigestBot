package embeddings

// PadToTargetDimensions pads or truncates a vector to the target dimensions.
// Zero-padding is mathematically safe for cosine similarity because
// zero values do not affect the angle between vectors.
func PadToTargetDimensions(vec []float32, target int) []float32 {
	if len(vec) == target {
		return vec
	}

	if len(vec) > target {
		// Truncate if vector is too long
		return vec[:target]
	}

	// Pad with zeros if vector is too short
	padded := make([]float32, target)
	copy(padded, vec)

	return padded
}
