package main

import (
	"fmt"
	"log"

	"github.com/ben-ranford/stave"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	row, err := stave.NewNode("atlas", "map", 1, stave.RolePanel, "Atlas", "Regions and signals")
	if err != nil {
		return err
	}
	row = row.WithStyleIntent(stave.IntentAccent).WithActions("inspect")
	root, err := stave.NewNode("atlas", "dashboard", 1, stave.RoleDashboard, "Atlas", "Calm operations", row)
	if err != nil {
		return err
	}
	root = root.WithStyleIntent(stave.IntentPrimary).WithActions("refresh")
	theme, err := stave.NewTheme("Atlas", map[stave.StyleIntent]string{
		stave.IntentPrimary: "North ",
		stave.IntentMuted:   "Slate ",
		stave.IntentAccent:  "Signal ",
		stave.IntentError:   "Fault ",
	})
	if err != nil {
		return err
	}
	out, err := (stave.Renderer{Caps: stave.Capabilities{Width: 100, Color: true}, Theme: theme}).RenderPlain(stave.Tree{Root: root, Revision: 1})
	if err != nil {
		return err
	}
	_, err = fmt.Print(out)
	return err
}
