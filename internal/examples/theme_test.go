package examples

import (
	"testing"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/theme"
)

func TestIndependentBrandThemesAcrossColourLadder(t *testing.T) {
	brands := []struct {
		name    string
		primary string
		banner  string
	}{
		{name: "lopper", primary: "#35d08f", banner: "SAP / EMBER / BLIGHT / LOAM"},
		{name: "atlas", primary: "#64c8ff", banner: "ATLAS / NORTH / SIGNAL"},
	}
	for _, level := range []capability.ColorLevel{capability.ColorTrueColor, capability.ColorANSI256, capability.ColorANSI16, capability.ColorMonochrome, capability.ColorNone} {
		hashes := map[string]string{}
		for _, brand := range brands {
			resolved, err := BrandTheme(brand.name, brand.primary, brand.banner).Resolve(theme.ModeDark, theme.DensityComfortable, capability.Manifest{Color: level, Unicode: capability.UnicodeFull})
			if err != nil {
				t.Fatalf("%s/%s: %v", brand.name, level, err)
			}
			hashes[brand.name] = resolved.HashString()
			if hashes[brand.name] == "" {
				t.Fatalf("%s/%s produced empty theme hash", brand.name, level)
			}
		}
		if hashes["lopper"] == hashes["atlas"] {
			t.Fatalf("%s collapsed independent brand themes to the same hash", level)
		}
	}
}
