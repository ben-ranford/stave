// Command atlas runs Stave's independent regression and proof client.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/ben-ranford/stave/internal/atlasrig"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("atlas", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	scenario := flags.String("scenario", "ready", "scenario to render")
	profile := flags.String("profile", "machine-json", "capability profile")
	format := flags.String("format", "auto", "auto, machine, plain, terminal, or artifact")
	matrix := flags.Bool("matrix", false, "emit the complete deterministic proof matrix")
	verify := flags.Bool("verify", false, "run all Atlas proof assertions")
	manifest := flags.Bool("manifest", false, "emit the scenario/profile manifest")
	list := flags.Bool("list", false, "list available scenarios and profiles")
	if err := flags.Parse(args); err != nil {
		return err
	}
	selected := 0
	for _, enabled := range []bool{*matrix, *verify, *manifest, *list} {
		if enabled {
			selected++
		}
	}
	if selected > 1 {
		return errors.New("choose only one of -matrix, -verify, -manifest, or -list")
	}
	rig, err := atlasrig.New()
	if err != nil {
		return err
	}
	switch {
	case *verify:
		if err := rig.Verify(ctx); err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "atlas proof rig: ok (%d scenarios x %d profiles)\n", len(rig.Manifest().Scenarios), len(rig.Manifest().Profiles))
		return err
	case *matrix:
		proof, err := rig.Matrix(ctx)
		if err != nil {
			return err
		}
		return writeJSON(output, proof)
	case *manifest:
		encoded, err := atlasrig.ManifestJSON(rig.Manifest())
		if err != nil {
			return err
		}
		_, err = output.Write(encoded)
		return err
	case *list:
		return writeList(output, rig.Manifest())
	}

	artifact, err := rig.Prepare(ctx, *scenario, *profile)
	if err != nil {
		return err
	}
	selectedFormat := strings.ToLower(strings.TrimSpace(*format))
	if selectedFormat == "auto" {
		switch {
		case artifact.OutputMode == "machine-json":
			selectedFormat = "machine"
		case artifact.TTY:
			selectedFormat = "terminal"
		default:
			selectedFormat = "plain"
		}
	}
	switch selectedFormat {
	case "machine":
		_, err = fmt.Fprintf(output, "%s\n", artifact.Machine)
	case "plain":
		_, err = fmt.Fprintf(output, "%s\n", artifact.Plain)
	case "terminal":
		_, err = fmt.Fprint(output, artifact.Terminal)
	case "artifact":
		err = writeJSON(output, artifact)
	default:
		return fmt.Errorf("unsupported Atlas output format %q", *format)
	}
	return err
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeList(output io.Writer, manifest atlasrig.Manifest) error {
	if _, err := fmt.Fprintln(output, "Scenarios:"); err != nil {
		return err
	}
	for _, scenario := range manifest.Scenarios {
		if _, err := fmt.Fprintf(output, "  %s [%s] - %s\n", scenario.Name, scenario.Tier, scenario.Description); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(output, "Profiles:"); err != nil {
		return err
	}
	for _, profile := range manifest.Profiles {
		if _, err := fmt.Fprintf(output, "  %s - %s\n", profile.Name, profile.Description); err != nil {
			return err
		}
	}
	return nil
}
