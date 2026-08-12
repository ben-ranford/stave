package width

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const PolicyVersion = "stave-width-v1"

type Policy struct {
	Version      string
	TabWidth     int
	InvalidWidth int
	EmojiWide    bool
}

type Cluster struct {
	Text    string
	Width   int
	Invalid bool
	Control bool
}

var DefaultPolicy = Policy{
	Version:      PolicyVersion,
	TabWidth:     4,
	InvalidWidth: 1,
	EmojiWide:    true,
}

func Rune(r rune) int {
	return DefaultPolicy.RuneWidth(r)
}

func String(s string) int {
	return DefaultPolicy.StringWidth(s)
}

func Measure(s string) int {
	return String(s)
}

func NormalizeTabs(s string, tabWidth int) string {
	return DefaultPolicy.withTabWidth(tabWidth).NormalizeTabs(s)
}

func Truncate(s string, limit int, indicator string) string {
	return DefaultPolicy.Truncate(s, limit, indicator)
}

func ASCIIOnly(s string) string {
	var b strings.Builder
	for len(s) > 0 {
		r, n := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && n == 1 {
			fmt.Fprintf(&b, "\\x%02x", s[0])
			s = s[1:]
			continue
		}
		if r == '\n' || r == '\t' || (r >= 0x20 && r <= 0x7e) {
			b.WriteRune(r)
		} else if r <= 0xffff {
			fmt.Fprintf(&b, "\\u%04X", r)
		} else {
			fmt.Fprintf(&b, "\\U%08X", r)
		}
		s = s[n:]
	}
	return b.String()
}

func (p Policy) withDefaults() Policy {
	if p.Version == "" {
		p.Version = PolicyVersion
	}
	if p.TabWidth <= 0 {
		p.TabWidth = DefaultPolicy.TabWidth
	}
	if p.InvalidWidth <= 0 {
		p.InvalidWidth = DefaultPolicy.InvalidWidth
	}
	return p
}

func (p Policy) withTabWidth(tabWidth int) Policy {
	p = p.withDefaults()
	if tabWidth > 0 {
		p.TabWidth = tabWidth
	}
	return p
}

func (p Policy) RuneWidth(r rune) int {
	p = p.withDefaults()
	switch {
	case r == '\t', r == '\n', r == '\r':
		return 0
	case isZeroWidth(r):
		return 0
	case isWide(r):
		return 2
	case p.EmojiWide && isEmoji(r):
		return 2
	default:
		return 1
	}
}

func (p Policy) StringWidth(s string) int {
	p = p.withDefaults()
	if measured, ok := p.asciiStringWidth(s); ok {
		return measured
	}
	col := 0
	for _, cluster := range p.Clusters(s) {
		if cluster.Text == "\t" {
			col += tabAdvance(col, p.TabWidth)
			continue
		}
		col += cluster.Width
	}
	return col
}

func (p Policy) asciiStringWidth(s string) (int, bool) {
	col := 0
	for index := 0; index < len(s); index++ {
		value := s[index]
		if value >= utf8.RuneSelf {
			return 0, false
		}
		switch value {
		case '\t':
			col += tabAdvance(col, p.TabWidth)
		case '\n', '\r':
		default:
			if value >= 0x20 && value != 0x7f {
				col++
			}
		}
	}
	return col, true
}

func (p Policy) NormalizeTabs(s string) string {
	p = p.withDefaults()
	var b strings.Builder
	col := 0
	for _, cluster := range p.Clusters(s) {
		if cluster.Text == "\t" {
			advance := tabAdvance(col, p.TabWidth)
			b.WriteString(strings.Repeat(" ", advance))
			col += advance
			continue
		}
		b.WriteString(cluster.Text)
		if cluster.Text == "\n" || cluster.Text == "\r" {
			col = 0
			continue
		}
		col += cluster.Width
	}
	return b.String()
}

func (p Policy) Truncate(s string, limit int, indicator string) string {
	p = p.withDefaults()
	if limit <= 0 {
		return ""
	}
	if p.StringWidth(s) <= limit {
		return s
	}
	if indicator == "" {
		indicator = "…"
	}
	indicator = p.NormalizeTabs(indicator)
	indicatorWidth := p.StringWidth(indicator)
	if indicatorWidth >= limit {
		return p.takeWidth(indicator, limit)
	}
	var b strings.Builder
	width := 0
	for _, cluster := range p.Clusters(s) {
		if cluster.Text == "\n" || cluster.Text == "\r" {
			break
		}
		if width+cluster.Width+indicatorWidth > limit {
			break
		}
		b.WriteString(cluster.Text)
		if cluster.Text == "\t" {
			width += tabAdvance(width, p.TabWidth)
		} else {
			width += cluster.Width
		}
	}
	b.WriteString(indicator)
	return b.String()
}

func (p Policy) takeWidth(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	var b strings.Builder
	width := 0
	for _, cluster := range p.Clusters(s) {
		if width+cluster.Width > limit {
			break
		}
		b.WriteString(cluster.Text)
		if cluster.Text == "\t" {
			width += tabAdvance(width, p.TabWidth)
		} else {
			width += cluster.Width
		}
	}
	return b.String()
}

func (p Policy) Clusters(s string) []Cluster {
	p = p.withDefaults()
	out := make([]Cluster, 0, len(s))
	for i := 0; i < len(s); {
		start := i
		r, n := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && n == 1 {
			out = append(out, Cluster{Text: string(utf8.RuneError), Width: p.InvalidWidth, Invalid: true})
			i++
			continue
		}
		i += n
		cluster := Cluster{Text: s[start:i], Width: p.RuneWidth(r), Control: isControl(r)}
		if r == '\t' {
			cluster.Width = 0
			out = append(out, cluster)
			continue
		}
		if r == '\n' || r == '\r' {
			cluster.Width = 0
			out = append(out, cluster)
			continue
		}
		if isRegionalIndicator(r) && i < len(s) {
			nr, nn := utf8.DecodeRuneInString(s[i:])
			if isRegionalIndicator(nr) {
				cluster.Text = s[start : i+nn]
				cluster.Width = 2
				i += nn
			}
		}
		for i < len(s) {
			nr, nn := utf8.DecodeRuneInString(s[i:])
			if nr == utf8.RuneError && nn == 1 {
				break
			}
			switch {
			case isCombining(nr), isVariationSelector(nr), isEmojiModifier(nr), nr == keycapCombining:
				cluster.Text = s[start : i+nn]
				i += nn
				continue
			case nr == zwj:
				cluster.Text = s[start : i+nn]
				i += nn
				if i < len(s) {
					fr, fn := utf8.DecodeRuneInString(s[i:])
					if fr == utf8.RuneError && fn == 1 {
						break
					}
					cluster.Text = s[start : i+fn]
					cluster.Width = max(cluster.Width, p.RuneWidth(fr))
					i += fn
				}
				continue
			default:
				goto done
			}
		}
	done:
		if cluster.Width == 0 && !cluster.Control && !cluster.Invalid && cluster.Text != "" {
			cluster.Width = 1
		}
		out = append(out, cluster)
	}
	return out
}

func tabAdvance(col, tabWidth int) int {
	if tabWidth <= 0 {
		tabWidth = DefaultPolicy.TabWidth
	}
	return tabWidth - (col % tabWidth)
}

const (
	zwj             = rune(0x200D)
	keycapCombining = rune(0x20E3)
)

func isControl(r rune) bool {
	return (r >= 0 && r < 0x20 && r != '\n' && r != '\t') || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

func isCombining(r rune) bool {
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r)
}

func isVariationSelector(r rune) bool {
	return (r >= 0xFE00 && r <= 0xFE0F) || (r >= 0xE0100 && r <= 0xE01EF)
}

func isEmojiModifier(r rune) bool {
	return r >= 0x1F3FB && r <= 0x1F3FF
}

func isRegionalIndicator(r rune) bool {
	return r >= 0x1F1E6 && r <= 0x1F1FF
}

func isZeroWidth(r rune) bool {
	return isControl(r) || isCombining(r) || isVariationSelector(r) || r == zwj
}

func isEmoji(r rune) bool {
	switch {
	case r >= 0x1F300 && r <= 0x1FAFF:
		return true
	case r >= 0x2600 && r <= 0x26FF:
		return true
	case r >= 0x2700 && r <= 0x27BF:
		return true
	case isRegionalIndicator(r):
		return true
	default:
		return false
	}
}

func isWide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F:
		return true
	case r == 0x2329 || r == 0x232A:
		return true
	case r >= 0x2E80 && r <= 0xA4CF && r != 0x303F:
		return true
	case r >= 0xAC00 && r <= 0xD7A3:
		return true
	case r >= 0xF900 && r <= 0xFAFF:
		return true
	case r >= 0xFE10 && r <= 0xFE19:
		return true
	case r >= 0xFE30 && r <= 0xFE6F:
		return true
	case r >= 0xFF00 && r <= 0xFF60:
		return true
	case r >= 0xFFE0 && r <= 0xFFE6:
		return true
	default:
		return false
	}
}
