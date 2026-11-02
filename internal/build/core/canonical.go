package core

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
)

// canonicalJSON serializes a tree of map[string]any and string values as RFC
// 8785 (JCS) canonical JSON. A build recipe holds only objects and strings, so
// the number and literal rules of JCS never apply; anything else is a
// programming error and panics.
func canonicalJSON(value any) string {
	var buf strings.Builder

	writeCanonical(&buf, value)

	return buf.String()
}

func writeCanonical(buf *strings.Builder, value any) {
	switch typed := value.(type) {
	case string:
		writeCanonicalString(buf, typed)
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}

		// JCS orders members by their UTF-16 code units, not by UTF-8 bytes.
		slices.SortFunc(keys, compareUTF16)

		buf.WriteByte('{')

		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}

			writeCanonicalString(buf, key)
			buf.WriteByte(':')
			writeCanonical(buf, typed[key])
		}

		buf.WriteByte('}')
	default:
		panic("canonicalJSON: unsupported value type")
	}
}

func compareUTF16(left, right string) int {
	return slices.Compare(utf16.Encode([]rune(left)), utf16.Encode([]rune(right)))
}

// writeCanonicalString applies the ECMAScript JSON.stringify escaping JCS
// mandates: only the quote, the backslash and C0 controls are escaped, the
// latter with the short forms where one exists and lowercase \u00xx otherwise.
func writeCanonicalString(buf *strings.Builder, value string) {
	buf.WriteByte('"')

	for _, ch := range value {
		switch ch {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if ch < 0x20 {
				hex := strconv.FormatInt(int64(ch), 16)
				buf.WriteString(`\u` + strings.Repeat("0", 4-len(hex)) + hex)

				continue
			}

			buf.WriteRune(ch)
		}
	}

	buf.WriteByte('"')
}
