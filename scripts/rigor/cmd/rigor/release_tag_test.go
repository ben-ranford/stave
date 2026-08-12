package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseTagValidation(t *testing.T) {
	t.Parallel()
	script := filepath.Join("..", "..", "check-release-tag.sh")
	tests := []struct {
		name       string
		tag        string
		wantOutput string
		wantValid  bool
	}{
		{name: "ga", tag: "v1.0.0", wantOutput: "ga", wantValid: true},
		{name: "release candidate", tag: "v1.0.0-rc.1", wantOutput: "prerelease", wantValid: true},
		{name: "rolling candidate", tag: "v1.0.0-rolling.20260812140734.g0149571", wantOutput: "prerelease", wantValid: true},
		{name: "build metadata", tag: "v1.0.0+build.7", wantOutput: "ga", wantValid: true},
		{name: "hyphenated build metadata", tag: "v1.0.0+build-7", wantOutput: "ga", wantValid: true},
		{name: "prerelease and build metadata", tag: "v1.0.0-rc.1+build.7", wantOutput: "prerelease", wantValid: true},
		{name: "empty", tag: "", wantValid: false},
		{name: "missing patch", tag: "v1.0", wantValid: false},
		{name: "major leading zero", tag: "v01.0.0", wantValid: false},
		{name: "minor leading zero", tag: "v1.01.0", wantValid: false},
		{name: "patch leading zero", tag: "v1.0.01", wantValid: false},
		{name: "empty prerelease identifier", tag: "v1.0.0-rc..1", wantValid: false},
		{name: "trailing prerelease separator", tag: "v1.0.0-rc.", wantValid: false},
		{name: "numeric prerelease leading zero", tag: "v1.0.0-rc.01", wantValid: false},
		{name: "numeric prerelease only leading zero", tag: "v1.0.0-01", wantValid: false},
		{name: "empty build identifier", tag: "v1.0.0+build..7", wantValid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := exec.Command("bash", script, tt.tag)
			output, err := cmd.CombinedOutput()
			if tt.wantValid && err != nil {
				t.Fatalf("validate %q: %v\n%s", tt.tag, err, output)
			}
			if !tt.wantValid {
				if err == nil {
					t.Fatalf("invalid tag %q was accepted with output %q", tt.tag, strings.TrimSpace(string(output)))
				}
				return
			}
			if got := strings.TrimSpace(string(output)); got != tt.wantOutput {
				t.Fatalf("validate %q output = %q, want %q", tt.tag, got, tt.wantOutput)
			}
		})
	}
}
