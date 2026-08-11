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

type reportSummary struct {
	Open int
}

func run() error {
	tree, err := adaptReport(reportSummary{Open: 12})
	if err != nil {
		return err
	}
	theme, err := stave.NewTheme("Sap-Ember-Blight-Loam", map[stave.StyleIntent]string{
		stave.IntentPrimary: "Sap ",
		stave.IntentMuted:   "Loam ",
		stave.IntentAccent:  "Ember ",
		stave.IntentError:   "Blight ",
	})
	if err != nil {
		return err
	}
	out, err := (stave.Renderer{Caps: stave.Capabilities{Width: 100, Color: true}, Theme: theme}).RenderPlain(tree)
	if err != nil {
		return err
	}
	_, err = fmt.Print(out)
	return err
}

// adaptReport is the application-owned seam: Lopper-shaped data becomes Stave
// semantics without placing report or effect policy in the framework.
func adaptReport(report reportSummary) (stave.Tree, error) {
	row, err := stave.NewNode("lopper", "inbox", 1, stave.RoleRecord, "Inbox", fmt.Sprintf("%d open", report.Open))
	if err != nil {
		return stave.Tree{}, err
	}
	row = row.WithStyleIntent(stave.IntentError).WithActions("open")
	root, err := stave.NewNode("lopper", "shell", 1, stave.RoleMasterDetail, "Lopper", "Focused work", row)
	if err != nil {
		return stave.Tree{}, err
	}
	root = root.WithStyleIntent(stave.IntentPrimary).WithActions("refresh")
	return stave.Tree{Root: root, Revision: 1}, nil
}
