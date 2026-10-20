// Command stave-performance-compare compares two saved performance reports.
// It never writes or refreshes a baseline.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ben-ranford/stave/performance"
)

const (
	exitSuccess    = 0
	exitRegression = 2
	exitInvalid    = 3
	exitUsage      = 64
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("stave-performance-compare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	baselinePath := flags.String("baseline", "", "checked-in baseline performance report")
	candidatePath := flags.String("candidate", "", "candidate performance report")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *baselinePath == "" || *candidatePath == "" {
		fmt.Fprintln(stderr, "usage: stave-performance-compare -baseline baseline.json -candidate candidate.json")
		return exitUsage
	}
	baseline, err := load(*baselinePath)
	if err != nil {
		return invalid(stdout)
	}
	candidate, err := load(*candidatePath)
	if err != nil {
		return invalid(stdout)
	}
	comparison, err := performance.Compare(baseline, candidate)
	if err != nil {
		return invalid(stdout)
	}
	if err := write(stdout, comparison); err != nil {
		return exitInvalid
	}
	if !comparison.Passed {
		return exitRegression
	}
	return exitSuccess
}

func load(path string) (performance.Report, error) {
	file, err := os.Open(path)
	if err != nil {
		return performance.Report{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, performance.MaxReportBytes+1))
	if err != nil {
		return performance.Report{}, err
	}
	return performance.DecodeReport(data)
}

func invalid(stdout io.Writer) int {
	_ = write(stdout, map[string]string{"status": "invalid", "error": "report failed bounded structural or compatibility validation"})
	return exitInvalid
}

func write(stdout io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, string(data))
	return err
}
