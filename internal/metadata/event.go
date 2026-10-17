package metadata

// Source identifies the metadata format that produced an event.
type Source string

// SourceICY identifies metadata carried in ICY blocks.
const SourceICY Source = "icy"

// EventKind distinguishes a complete track snapshot from an explicit clear.
type EventKind uint8

const (
	// Replace replaces every track field, including fields absent from the snapshot.
	Replace EventKind = iota + 1
	// Clear removes the current track. Its Track field is ignored.
	Clear
)

// Event reports musical information at the stream's current read position.
// Producers invoke the callback when the update takes effect, rather than when
// a future segment is downloaded. Missing or malformed metadata produces no
// callback. An empty Replace snapshot is ignored; clearing requires Clear.
type Event struct {
	Kind   EventKind
	Source Source
	Track  Track
}
