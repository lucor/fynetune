package radio

// SplitTitle preserves the original helper used by the UI while keeping title
// normalization in the player domain rather than in the ICY transport.
func SplitTitle(value string) (string, string) {
	metadata := ParseTrackMetadata(value)
	return metadata.Artist, metadata.Title
}
