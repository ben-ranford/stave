package lipglossadapter

import (
	"github.com/ben-ranford/stave/capability"
	"github.com/charmbracelet/colorprofile"
)

type Profile struct {
	OutputMode     capability.OutputMode
	ColorLevel     capability.ColorLevel
	TTY            bool
	DarkBackground bool
}

func ProfileFromManifest(m capability.Manifest, darkBackground bool) Profile {
	return Profile{
		OutputMode:     m.OutputMode,
		ColorLevel:     m.Color,
		TTY:            m.TTY,
		DarkBackground: darkBackground,
	}
}

func (p Profile) RenderANSI() bool {
	if !p.TTY {
		return false
	}
	switch p.OutputMode {
	case capability.OutputMachineJSON, capability.OutputMachineJSONL, capability.OutputPlain, capability.OutputDumb:
		return false
	}
	return p.ColorLevel != capability.ColorNone
}

func (p Profile) ColorProfile() colorprofile.Profile {
	switch p.ColorLevel {
	case capability.ColorTrueColor:
		return colorprofile.TrueColor
	case capability.ColorANSI256:
		return colorprofile.ANSI256
	case capability.ColorANSI16:
		return colorprofile.ANSI
	case capability.ColorMonochrome:
		return colorprofile.ASCII
	default:
		return colorprofile.NoTTY
	}
}
