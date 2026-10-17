package id3

import (
	"encoding/binary"
	"unicode/utf16"
	"unicode/utf8"
)

// decodeText consumes the ID3 encoding byte and decodes the remaining payload.
// Null separators are preserved for TXXX descriptions and multiple values.
// Invalid UTF-8, missing UTF-16 byte order marks, truncated code units, and
// unpaired surrogates reject the frame rather than introducing replacement text.
func decodeText(data []byte) (string, bool) {
	if len(data) == 0 {
		return "", false
	}
	encoding, data := data[0], data[1:]
	switch encoding {
	case 0: // ISO-8859-1.
		runes := make([]rune, len(data))
		for i, b := range data {
			runes[i] = rune(b)
		}
		return string(runes), true
	case 1, 2: // UTF-16 with BOM, or UTF-16BE without BOM.
		var order binary.ByteOrder = binary.BigEndian
		if encoding == 1 {
			if len(data) < 2 {
				return "", false
			}
			switch {
			case data[0] == 0xff && data[1] == 0xfe:
				order = binary.LittleEndian
			case data[0] == 0xfe && data[1] == 0xff:
			default:
				return "", false
			}
			data = data[2:]
		}
		if len(data)%2 != 0 {
			return "", false
		}
		words := make([]uint16, len(data)/2)
		for i := range words {
			words[i] = order.Uint16(data[i*2:])
		}
		for i := 0; i < len(words); i++ {
			word := words[i]
			if word >= 0xd800 && word <= 0xdbff {
				if i+1 >= len(words) || words[i+1] < 0xdc00 || words[i+1] > 0xdfff {
					return "", false
				}
				i++
			} else if word >= 0xdc00 && word <= 0xdfff {
				return "", false
			}
		}
		return string(utf16.Decode(words)), true
	case 3: // UTF-8.
		return string(data), utf8.Valid(data)
	default:
		return "", false
	}
}
