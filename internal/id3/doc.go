// Package id3 parses text frames from ID3v2.3 and ID3v2.4 tags.
//
// Parse accepts a byte slice beginning with one complete tag. Tag.Size reports
// the bytes occupied by the tag so the caller can locate any following audio
// data or tags. Parsing neither modifies the input nor performs any I/O.
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
// Values are decoded without inferring musical fields or interpreting provider
// payloads. In particular, JSON in TEXT remains text. All non-text frames are
// skipped, including PRIV transport timestamps and attached pictures.
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
