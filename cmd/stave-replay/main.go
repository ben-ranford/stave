// Command stave-replay validates and compares saved replay transcripts.
// It compares recorded evidence only; it does not re-execute an application
// model because replay execution requires a caller-supplied replay.ApplyFunc.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ben-ranford/stave/replay"
)

const (
	exitSuccess  = 0
	exitMismatch = 2
	exitInvalid  = 3
	exitUsage    = 64
)

type report struct {
	Status     string             `json:"status"`
	Evidence   string             `json:"evidence"`
	Divergence *replay.Divergence `json:"divergence,omitempty"`
	Error      string             `json:"error,omitempty"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return usage(stderr)
	}
	switch args[0] {
	case "validate":
		flags := flag.NewFlagSet("validate", flag.ContinueOnError)
		flags.SetOutput(stderr)
		input := flags.String("input", "", "canonical transcript JSON file")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *input == "" {
			return usage(stderr)
		}
		if _, err := load(*input); err != nil {
			writeReport(stdout, report{Status: "invalid", Evidence: evidenceLabel, Error: invalidInputMessage})
			return exitInvalid
		}
		writeReport(stdout, report{Status: "valid", Evidence: evidenceLabel})
		return exitSuccess
	case "compare":
		flags := flag.NewFlagSet("compare", flag.ContinueOnError)
		flags.SetOutput(stderr)
		expectedPath := flags.String("expected", "", "expected canonical transcript JSON file")
		actualPath := flags.String("actual", "", "actual canonical transcript JSON file")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *expectedPath == "" || *actualPath == "" {
			return usage(stderr)
		}
		expected, err := load(*expectedPath)
		if err != nil {
			writeReport(stdout, report{Status: "invalid", Evidence: evidenceLabel, Error: invalidInputMessage})
			return exitInvalid
		}
		actual, err := load(*actualPath)
		if err != nil {
			writeReport(stdout, report{Status: "invalid", Evidence: evidenceLabel, Error: invalidInputMessage})
			return exitInvalid
		}
		if err := replay.Validate(expected, actual); err != nil {
			var divergence *replay.Divergence
			if errors.As(err, &divergence) {
				writeReport(stdout, report{Status: "mismatch", Evidence: evidenceLabel, Divergence: replay.RedactedDivergence(divergence)})
			} else {
				writeReport(stdout, report{Status: "invalid", Evidence: evidenceLabel, Error: invalidInputMessage})
				return exitInvalid
			}
			return exitMismatch
		}
		writeReport(stdout, report{Status: "match", Evidence: evidenceLabel})
		return exitSuccess
	default:
		return usage(stderr)
	}
}

const evidenceLabel = "saved transcript evidence comparison only; no application model was re-executed"
const invalidInputMessage = "transcript failed bounded structural validation"

func load(path string) (replay.Transcript, error) {
	file, err := os.Open(path)
	if err != nil {
		return replay.Transcript{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, replay.MaxTranscriptBytes+1))
	if err != nil {
		return replay.Transcript{}, err
	}
	return replay.DecodeTranscript(data)
}

func writeReport(writer io.Writer, value report) {
	data, err := json.Marshal(value)
	if err != nil {
		fmt.Fprintf(writer, "{\"status\":\"invalid\",\"error\":%q}\n", err.Error())
		return
	}
	fmt.Fprintln(writer, string(data))
}

func usage(stderr io.Writer) int {
	fmt.Fprintln(stderr, "usage: stave-replay validate -input transcript.json | stave-replay compare -expected expected.json -actual actual.json")
	return exitUsage
}
