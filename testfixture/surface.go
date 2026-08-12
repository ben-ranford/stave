package testfixture

import (
	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/surface"
)

// Surface returns the adapter-neutral fixture used by every renderer adapter.
// Keeping it in the root module makes adapter parity a shared contract rather
// than a set of unrelated local goldens.
func Surface() (surface.Surface, error) {
	fixture := surface.New(16, 2)
	style := surface.ResolvedStyle{Foreground: "ansi16:97", Background: "ansi16:30", Bold: true}
	return fixture.WithText(0, 0, "Stave", style, "", "", 0, layout.Rect{Width: 16, Height: 2})
}
