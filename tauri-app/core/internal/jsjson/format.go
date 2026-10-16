package jsjson

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
)

// FormatNumber prints f as JavaScript's String(n) does.
func FormatNumber(f float64) string {
	if f == 0 {
		return "0" // also -0
	}
	if math.IsNaN(f) {
		return "NaN"
	}
	if math.IsInf(f, 0) {
		if f < 0 {
			return "-Infinity"
		}
		return "Infinity"
	}
	if a := math.Abs(f); a >= 1e-6 && a < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	s := strconv.FormatFloat(f, 'e', -1, 64) // "1.5e-07"
	mant, exp, _ := strings.Cut(s, "e")
	return mant + "e" + exp[:1] + strings.TrimLeft(exp[1:], "0")
}

// Stringify is JSON.stringify(v) with no spaces.
func Stringify(v Value) string {
	var sb strings.Builder
	write(&sb, v, "", 0)
	return sb.String()
}

// Indent is JSON.stringify(v, null, 2).
func Indent(v Value) []byte {
	var sb strings.Builder
	write(&sb, v, "  ", 0)
	return []byte(sb.String())
}

// write emits v. An empty unit means compact; otherwise pretty with unit indent per level.
func write(sb *strings.Builder, v Value, unit string, depth int) {
	switch v.Kind {
	case Bool:
		sb.WriteString(strconv.FormatBool(v.B))
	case Number:
		if math.IsNaN(v.N) || math.IsInf(v.N, 0) {
			sb.WriteString("null")
		} else {
			sb.WriteString(FormatNumber(v.N))
		}
	case String:
		quote(sb, v.S)
	case Array:
		if len(v.A) == 0 {
			sb.WriteString("[]")
			return
		}
		sb.WriteByte('[')
		for i, e := range v.A {
			if i > 0 {
				sb.WriteByte(',')
			}
			newline(sb, unit, depth+1)
			write(sb, e, unit, depth+1)
		}
		newline(sb, unit, depth)
		sb.WriteByte(']')
	case Object:
		n := 0
		for _, k := range v.O.Keys {
			if v.O.Get(k).Kind == Undefined {
				continue // JSON.stringify drops undefined properties
			}
			if n == 0 {
				sb.WriteByte('{')
			} else {
				sb.WriteByte(',')
			}
			n++
			newline(sb, unit, depth+1)
			quote(sb, k)
			sb.WriteByte(':')
			if unit != "" {
				sb.WriteByte(' ')
			}
			write(sb, v.O.Get(k), unit, depth+1)
		}
		if n == 0 {
			sb.WriteString("{}")
			return
		}
		newline(sb, unit, depth)
		sb.WriteByte('}')
	default: // Null, and Undefined inside an array
		sb.WriteString("null")
	}
}

func newline(sb *strings.Builder, unit string, depth int) {
	if unit == "" {
		return
	}
	sb.WriteByte('\n')
	sb.WriteString(strings.Repeat(unit, depth))
}

// quote escapes only ", \ and runes below 0x20, like JSON.stringify.
// ponytail: Go strings cannot hold lone surrogates, so invalid UTF-8 is written as U+FFFD where
// JavaScript would write \udXXX; add a WTF-8 decoder if Garmin ever sends one.
func quote(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			sb.WriteString(`\"`)
		case r == '\\':
			sb.WriteString(`\\`)
		case r == '\b':
			sb.WriteString(`\b`)
		case r == '\f':
			sb.WriteString(`\f`)
		case r == '\n':
			sb.WriteString(`\n`)
		case r == '\r':
			sb.WriteString(`\r`)
		case r == '\t':
			sb.WriteString(`\t`)
		case r < 0x20:
			sb.WriteString(`\u00`)
			sb.WriteString(strconv.FormatInt(int64(r)|0x100, 16)[1:])
		default:
			sb.WriteRune(r) // invalid bytes arrive as RuneError and print U+FFFD
		}
	}
	sb.WriteByte('"')
}

// Cell is String(v) for a CSV cell: "" for Undefined and Null.
func Cell(v Value) string {
	switch v.Kind {
	case Bool:
		return strconv.FormatBool(v.B)
	case Number:
		return FormatNumber(v.N)
	case String:
		return v.S
	case Array:
		parts := make([]string, len(v.A))
		for i, e := range v.A {
			parts[i] = Cell(e) // Array.prototype.join maps null and undefined to ""
		}
		return strings.Join(parts, ",")
	case Object:
		return "[object Object]"
	}
	return ""
}

// SliceUTF16 is JavaScript's s.slice(0, n): n UTF-16 code units. A surrogate pair cut in half
// decodes to U+FFFD, where JavaScript keeps a lone surrogate; nothing here encodes such a string again.
func SliceUTF16(s string, n int) string {
	u := utf16.Encode([]rune(s))
	if n < len(u) {
		u = u[:n]
	}
	return string(utf16.Decode(u))
}
