package lipglossadapter

type TokenID string
type Mode string
type Density string
type Kind string

const KindColor Kind = "color"

type ResolvedValue struct {
	Value any
	Kind  Kind
}

type ResolvedTheme struct {
	ThemeID string
	Version string
	Mode    Mode
	Density Density
	Tokens  map[TokenID]ResolvedValue
}

func (r ResolvedTheme) Token(id TokenID) (ResolvedValue, bool) {
	v, ok := r.Tokens[id]
	return v, ok
}
