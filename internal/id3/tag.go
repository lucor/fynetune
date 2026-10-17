package id3

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
)

// MaxTagSize bounds the total size of one tag, including its header and footer.
const MaxTagSize = 256 << 10

var (
	// ErrNoTag indicates that data does not begin with the ID3 identifier.
	ErrNoTag = errors.New("ID3 tag not found")
	// ErrMalformed indicates invalid tag framing, sizes, padding, or headers.
	ErrMalformed = errors.New("malformed ID3 tag")
	// ErrTooLarge indicates that the declared tag size exceeds MaxTagSize.
	ErrTooLarge = errors.New("ID3 tag exceeds size limit")
	// ErrUnsupportedVersion indicates a version other than ID3v2.3 or ID3v2.4.
	ErrUnsupportedVersion = errors.New("unsupported ID3 version")
)

// Tag contains text frames only. Private, picture, compressed, and encrypted
// frames are skipped. Frame identifiers and user-defined descriptions retain
// their original spelling; interpreting their values belongs to the caller.
type Tag struct {
	Size           int                 // Total bytes consumed, including header and footer.
	IsUpdate       bool                // ID3v2.4 extended-header update flag.
	TextFrames     map[string][]string // Standard text frames, keyed by frame identifier.
	UserTextFrames map[string][]string // TXXX values, keyed by description.
}

// Parse reads one tag at the beginning of data. Bytes after the tag are ignored.
// Incomplete or structurally malformed tags return no partial result. Invalid
// text encodings and unsupported frame formats are skipped independently.
// Once the ID3 identifier is present, truncated input returns io.ErrUnexpectedEOF
// unless the available header already establishes another error, such as an
// unsupported version or an excessive declared size.
func Parse(data []byte) (Tag, error) {
	if len(data) < 3 || string(data[:3]) != "ID3" {
		return Tag{}, ErrNoTag
	}
	if len(data) < 10 {
		return Tag{}, io.ErrUnexpectedEOF
	}
	version := data[3]
	if version != 3 && version != 4 {
		return Tag{}, ErrUnsupportedVersion
	}
	allowedFlags := byte(0xe0)
	if version == 4 {
		allowedFlags = 0xf0
	}
	if data[4] == 0xff || data[5]&^allowedFlags != 0 {
		return Tag{}, ErrMalformed
	}
	size, ok := syncsafe(data[6:10])
	if !ok {
		return Tag{}, ErrMalformed
	}
	total := 10 + size
	footer := version == 4 && data[5]&0x10 != 0
	if footer {
		total += 10
	}
	if total > MaxTagSize {
		return Tag{}, ErrTooLarge
	}
	if total > len(data) {
		return Tag{}, io.ErrUnexpectedEOF
	}
	if footer && (string(data[total-10:total-7]) != "3DI" || !bytes.Equal(data[total-7:total], data[3:10])) {
		return Tag{}, ErrMalformed
	}
	body := data[10 : 10+size]
	if version == 3 && data[5]&0x80 != 0 {
		body = removeUnsynchronisation(body)
	}
	tag := Tag{Size: total}
	if data[5]&0x40 != 0 {
		var err error
		body, tag.IsUpdate, err = skipExtendedHeader(body, version)
		if err != nil {
			return Tag{}, err
		}
	}
	for len(body) > 0 {
		if body[0] == 0 {
			if !allZero(body) {
				return Tag{}, ErrMalformed
			}
			break
		}
		if len(body) < 10 || !validFrameID(body[:4]) {
			return Tag{}, ErrMalformed
		}
		frameSize := binary.BigEndian.Uint32(body[4:8])
		if version == 4 {
			var n int
			n, ok = syncsafe(body[4:8])
			if !ok {
				return Tag{}, ErrMalformed
			}
			frameSize = uint32(n)
		}
		if frameSize > uint32(len(body)-10) {
			return Tag{}, ErrMalformed
		}
		id := string(body[:4])
		status, flags := body[8], body[9]
		payload := body[10 : 10+int(frameSize)]
		body = body[10+int(frameSize):]
		if id[0] != 'T' {
			continue
		}
		payload, ok = textPayload(payload, version, status, flags, data[5]&0x80 != 0)
		if !ok {
			continue
		}
		value, ok := decodeText(payload)
		if !ok {
			continue
		}
		if id == "TXXX" {
			description, text, found := strings.Cut(value, "\x00")
			if found {
				if tag.UserTextFrames == nil {
					tag.UserTextFrames = make(map[string][]string)
				}
				tag.UserTextFrames[description] = append(tag.UserTextFrames[description], splitValues(text)...)
			}
		} else {
			if tag.TextFrames == nil {
				tag.TextFrames = make(map[string][]string)
			}
			tag.TextFrames[id] = append(tag.TextFrames[id], splitValues(value)...)
		}
	}
	return tag, nil
}

// syncsafe decodes an integer whose bytes each carry seven significant bits.
// A set high bit is invalid rather than part of the integer.
func syncsafe(data []byte) (int, bool) {
	n := 0
	for _, b := range data {
		if b&0x80 != 0 {
			return 0, false
		}
		n = n<<7 | int(b)
	}
	return n, true
}

// validFrameID checks that an identifier contains only uppercase ASCII letters
// or digits. The caller supplies the four bytes used by ID3v2.3 and ID3v2.4.
func validFrameID(data []byte) bool {
	for _, b := range data {
		if (b < 'A' || b > 'Z') && (b < '0' || b > '9') {
			return false
		}
	}
	return true
}

// allZero distinguishes tag padding from malformed bytes after a zero frame ID.
func allZero(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

// skipExtendedHeader preserves the v2.4 update flag. Checksums and restrictions
// describe the source tag and are skipped; this parser does not verify CRCs.
// The v2.3 size excludes its own four bytes, while the v2.4 size includes them.
func skipExtendedHeader(data []byte, version byte) ([]byte, bool, error) {
	if len(data) < 4 {
		return nil, false, ErrMalformed
	}
	if version == 3 {
		size := binary.BigEndian.Uint32(data[:4])
		if size < 6 || size > uint32(len(data)-4) {
			return nil, false, ErrMalformed
		}
		return data[4+int(size):], false, nil
	}
	size, ok := syncsafe(data[:4])
	if !ok || size < 6 || size > len(data) || data[4] != 1 || data[5]&^byte(0x70) != 0 {
		return nil, false, ErrMalformed
	}
	extra := data[6:size]
	for _, flag := range []struct{ mask, size byte }{{0x40, 0}, {0x20, 5}, {0x10, 1}} {
		if data[5]&flag.mask == 0 {
			continue
		}
		if len(extra) < 1+int(flag.size) || extra[0] != flag.size {
			return nil, false, ErrMalformed
		}
		extra = extra[1+int(flag.size):]
	}
	if len(extra) != 0 {
		return nil, false, ErrMalformed
	}
	return data[size:], data[5]&0x40 != 0, nil
}

// textPayload removes supported frame-format prefixes before text decoding.
// Unknown flags, compression, encryption, and inconsistent length indicators
// make a frame unreadable. In v2.4, tag and frame unsynchronisation flags refer
// to the same transformation, which must be undone only once.
func textPayload(data []byte, version, status, flags byte, tagUnsynchronised bool) ([]byte, bool) {
	grouped, lengthIndicator := false, false
	if version == 3 {
		if status&^byte(0xe0) != 0 || flags&^byte(0xe0) != 0 || flags&0xc0 != 0 {
			return nil, false
		}
		grouped = flags&0x20 != 0
	} else {
		if status&^byte(0x70) != 0 || flags&^byte(0x4f) != 0 || flags&0x0c != 0 {
			return nil, false
		}
		if tagUnsynchronised || flags&0x02 != 0 {
			data = removeUnsynchronisation(data)
		}
		grouped, lengthIndicator = flags&0x40 != 0, flags&0x01 != 0
	}
	if grouped {
		if len(data) == 0 {
			return nil, false
		}
		data = data[1:]
	}
	if lengthIndicator {
		if len(data) < 4 {
			return nil, false
		}
		size, ok := syncsafe(data[:4])
		if !ok || size != len(data)-4 {
			return nil, false
		}
		data = data[4:]
	}
	return data, true
}

// removeUnsynchronisation removes the zero inserted after an FF byte to avoid
// MPEG sync patterns. It returns the original slice when no removal is needed,
// and otherwise allocates a decoded copy without modifying the input.
func removeUnsynchronisation(data []byte) []byte {
	if !bytes.Contains(data, []byte{0xff, 0}) {
		return data
	}
	result := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		result = append(result, data[i])
		if data[i] == 0xff && i+1 < len(data) && data[i+1] == 0 {
			i++
		}
	}
	return result
}

// splitValues separates null-delimited text values, removes trailing null
// padding, and strips a repeated UTF-16 byte order mark from each value.
// Whitespace and slashes remain part of the text for the caller to interpret.
func splitValues(value string) []string {
	value = strings.TrimRight(value, "\x00")
	if value == "" {
		return nil
	}
	values := strings.Split(value, "\x00")
	for i := range values {
		values[i] = strings.TrimPrefix(values[i], "\ufeff")
	}
	return values
}
