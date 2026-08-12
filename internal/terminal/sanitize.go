package terminal

import (
	"bytes"
	"io"
	"unicode/utf8"
)

const SanitizerVersion = "stave-terminal-sink-v1"

type Writer struct {
	dst io.Writer
}

func NewWriter(dst io.Writer) Writer {
	return Writer{dst: dst}
}

func (w Writer) Write(p []byte) (int, error) {
	if w.dst == nil {
		return len(p), nil
	}
	safe := SanitizeBytes(p)
	_, err := w.dst.Write(safe)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w Writer) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func Sanitize(s string) string {
	return string(SanitizeBytes([]byte(s)))
}

func SanitizeBytes(in []byte) []byte {
	out := bytes.NewBuffer(make([]byte, 0, len(in)))
	for i := 0; i < len(in); {
		switch b := in[i]; {
		case b == 0x1b:
			i = skipEscape(in, i)
		case b >= 0x80 && b <= 0x9f:
			i++
		case b < 0x20:
			if b == '\n' || b == '\t' {
				out.WriteByte(b)
			}
			i++
		default:
			r, size := utf8.DecodeRune(in[i:])
			if r == utf8.RuneError && size == 1 {
				out.WriteRune(utf8.RuneError)
				i++
				continue
			}
			out.WriteRune(r)
			i += size
		}
	}
	return out.Bytes()
}

func skipEscape(in []byte, i int) int {
	if i+1 >= len(in) {
		return len(in)
	}
	switch in[i+1] {
	case '[':
		return skipCSI(in, i+2)
	case ']':
		return skipOSC(in, i+2)
	case 'P', '^', '_', 'X':
		return skipST(in, i+2)
	default:
		return i + 2
	}
}

func skipCSI(in []byte, i int) int {
	for i < len(in) {
		b := in[i]
		if b >= 0x40 && b <= 0x7e {
			return i + 1
		}
		i++
	}
	return len(in)
}

func skipOSC(in []byte, i int) int {
	for i < len(in) {
		if in[i] == 0x07 {
			return i + 1
		}
		if in[i] == 0x1b && i+1 < len(in) && in[i+1] == '\\' {
			return i + 2
		}
		i++
	}
	return len(in)
}

func skipST(in []byte, i int) int {
	for i < len(in) {
		if in[i] == 0x1b && i+1 < len(in) && in[i+1] == '\\' {
			return i + 2
		}
		i++
	}
	return len(in)
}
