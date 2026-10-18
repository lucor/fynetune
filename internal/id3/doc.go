// Package id3 parses text frames from ID3v2.3 and ID3v2.4 tags.
//
// Parse accepts a byte slice beginning with one complete tag. Tag.Size reports
// the bytes occupied by the tag so the caller can locate any following audio
// data or tags. Parse neither modifies the input nor performs any I/O.
//
// # Text frames
//
// Tag.TextFrames preserves standard text frame identifiers, including TPE1
// (artist), TIT2 (title), TALB (album), TYER or TDRC (date), TIT1 (content grouping),
// and TEXT (lyricist or text writer). TXXX values are exposed separately in
// Tag.UserTextFrames, keyed by their descriptions. Null-separated values and
// repeated frames are collected in encounter order. Slash-separated text is
// preserved as a single value.
//
// The parser supports ISO-8859-1, UTF-16 with a byte order mark, UTF-16BE, and
// UTF-8. It handles tag and frame unsynchronisation, extended headers, grouping
// identity bytes, data length indicators, and ID3v2.4 footers. Tag.IsUpdate
// preserves the ID3v2.4 extended-header update flag; callers decide how to apply
// a partial update to previously received metadata.
//
// Parse decodes values without inferring musical fields or interpreting provider
// payloads. In particular, JSON in TEXT remains text. All non-text frames are
// skipped, including PRIV transport timestamps and attached pictures.
//
// # Track normalization
//
// Tag.Track maps decoded source fields to the shared metadata.Track model. Artist,
// title, and album come from TPE1, TIT2, and TALB. Release year uses a valid date
// from TDRL, TDRC, or TYER, in that order. Multiple artists are comma-separated.
// When TIT2 is missing, TIT1 or TXXX grouping supplies unstructured artist/title
// text using the same generic separator rules as ICY. Standard fields take
// precedence. TEXT is not interpreted as a title or as provider-specific JSON.
// Each call returns the tag's own fields; IsUpdate does not merge prior state.
//
// At debug level, normalization logs decoded input, the resulting track, and
// reasons for ignoring fields. Diagnostic text is bounded, while the actual
// metadata retains its complete values.
//
// # Limits and errors
//
// MaxTagSize bounds the entire tag, including its header and optional footer.
// Unsupported versions, excessive sizes, incomplete input, and invalid tag
// structure return an error and an empty Tag. Individual text frames with an
// invalid encoding or unsupported flags are skipped so other frames can still
// be read. Compressed and encrypted frames are skipped; CRCs are not verified.
//
// The tag structures are described by the ID3 specifications:
// https://id3.org/id3v2.3.0 and https://id3.org/id3v2.4.0-structure.
// Frame definitions are at https://id3.org/id3v2.4.0-frames.
package id3
