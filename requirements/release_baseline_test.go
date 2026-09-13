package requirements

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseBaselineStopsOnTargetFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release Makefile requires a POSIX host")
	}
	for _, program := range []string{"make", "bash"} {
		if _, err := exec.LookPath(program); err != nil {
			t.Fatalf("release checks require %s: %v", program, err)
		}
	}
	for _, failedTarget := range []string{"linux/amd64", "darwin/amd64", "darwin/arm64", "windows/amd64", ""} {
		t.Run("failure="+failedTarget, func(t *testing.T) {
			directory := t.TempDir()
			stub, calls := filepath.Join(directory, "go-stub"), filepath.Join(directory, "calls")
			script := `#!/usr/bin/env bash
if [[ "$1" != run ]]; then exit 0; fi
platform= architecture=
while (( $# )); do
  case "$1" in
    --goos) platform="$2"; shift ;;
    --goarch) architecture="$2"; shift ;;
  esac
  shift
done
target="$platform/$architecture"
printf '%s\n' "$target" >> "$STAVE_BASELINE_CALLS"
if [[ "$target" == "$STAVE_BASELINE_FAILURE" ]]; then exit 23; fi
`
			if err := os.WriteFile(stub, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			// Older make versions ignore .SHELLFLAGS; the recipe must propagate
			// failures itself even when global shell errexit is unavailable.
			command := exec.Command("make", "release-baseline", "GO="+stub, ".SHELLFLAGS=-c")
			command.Dir = ".."
			command.Env = append(os.Environ(), "STAVE_BASELINE_CALLS="+calls, "STAVE_BASELINE_FAILURE="+failedTarget)
			output, err := command.CombinedOutput()
			if (err != nil) != (failedTarget != "") {
				t.Fatalf("failure %q: make error=%v, output=%s", failedTarget, err, output)
			}
			data, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			want := releaseBaselineTargetPrefix(failedTarget)
			if got := strings.TrimSpace(string(data)); got != strings.Join(want, "\n") {
				t.Fatalf("target calls=%q, want %q", got, want)
			}
		})
	}
}

func releaseBaselineTargetPrefix(failedTarget string) []string {
	targets := []string{"linux/amd64", "darwin/amd64", "darwin/arm64", "windows/amd64"}
	for i, target := range targets {
		if target == failedTarget {
			return targets[:i+1]
		}
	}
	return targets
}
