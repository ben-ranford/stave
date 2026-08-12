package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ben-ranford/stave"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/internal/examples"
	"github.com/ben-ranford/stave/layout"
	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/render"
	"github.com/ben-ranford/stave/semantic"
)

type report struct{ Dependencies, Warnings int }

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	program := lopperProgram()
	prepared, err := program.NewSession(context.Background(), stave.SessionOptions{SessionID: "lopper-example", RuntimeDetected: lopperCapabilities()})
	if err != nil {
		return err
	}
	defer prepared.Session.Close()
	result, err := renderLopper(prepared)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(os.Stdout, terminalFixture(result.Terminal))
	return err
}

func terminalFixture(output string) string {
	return strings.TrimRight(output, " \n") + "\n"
}

func lopperProgram() stave.Program[report] {
	return stave.Program[report]{
		Initial: report{Dependencies: 7, Warnings: 2},
		Reduce: func(_ stave.ReduceContext, current report, _ event.Event) (report, []effect.Request, error) {
			return current, nil, nil
		},
		View: func(ctx stave.ViewContext, model report) (semantic.Tree, error) {
			title, err := primitive.Heading(primitive.Options{Namespace: "lopper", View: "summary", Entity: "title", Name: "Lopper"}, "Dependency summary")
			if err != nil {
				return semantic.Tree{}, err
			}
			deps, err := primitive.Status(primitive.Options{Namespace: "lopper", View: "summary", Entity: "dependencies", Name: "Dependencies", StyleRole: "success"}, fmt.Sprintf("%d dependencies", model.Dependencies))
			if err != nil {
				return semantic.Tree{}, err
			}
			warnings, err := primitive.Alert(primitive.Options{Namespace: "lopper", View: "summary", Entity: "warnings", Name: "Warnings", StyleRole: "advisory"}, fmt.Sprintf("%d warnings", model.Warnings))
			if err != nil {
				return semantic.Tree{}, err
			}
			root, err := primitive.Stack(primitive.Options{Namespace: "lopper", View: "summary", Entity: "root", Name: "Lopper", Metadata: map[string]string{"layout.kind": "stack"}}, title, deps, warnings)
			if err != nil {
				return semantic.Tree{}, err
			}
			return semantic.NewTree(max(1, ctx.Revision), root)
		},
		Theme: examples.BrandTheme("lopper", "#35d08f", "SAP / EMBER / BLIGHT / LOAM"),
	}
}

func lopperCapabilities() capability.Manifest {
	return capability.DetectEnv(map[string]string{"COLORTERM": "truecolor", "TERM": "xterm-256color"}, true, 100, 24)
}

func renderLopper(prepared *stave.Prepared[report]) (render.Result, error) {
	snapshot, err := prepared.Session.Snapshot()
	if err != nil {
		return render.Result{}, err
	}
	return render.Render(render.Request{Tree: snapshot.Tree, Theme: prepared.Theme, Capabilities: prepared.Capabilities, Viewport: layout.Size{Width: 100, Height: 24}})
}
