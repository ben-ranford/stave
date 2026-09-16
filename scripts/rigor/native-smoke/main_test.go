package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSelectedTestsRequirePassingRoots(t *testing.T) {
	output := []byte(`{"Action":"run","Test":"TestRuntimeRestoresOnEOFAndClosesOnce"}
{"Action":"pass","Test":"TestRuntimeRestoresOnEOFAndClosesOnce"}
{"Action":"run","Test":"TestCrossPlatformTerminalColourDetection/linux_truecolor"}
{"Action":"run","Test":"TestCrossPlatformTerminalColourDetection"}
{"Action":"pass","Test":"TestCrossPlatformTerminalColourDetection"}
`)
	seen, err := selectedTestPasses(output)
	if err != nil {
		t.Fatal(err)
	}
	if !seen["TestRuntimeRestoresOnEOFAndClosesOnce"] || !seen["TestCrossPlatformTerminalColourDetection"] {
		t.Fatalf("expected root test events, got %#v", seen)
	}
	if seen["TestLineDriverEmitsCanonicalTextAndShutdown"] {
		t.Fatalf("unexpected test event counted: %#v", seen)
	}
}

func TestSelectedTestsRejectSkippedOrFailedRootsAndChildren(t *testing.T) {
	for _, action := range []string{"skip", "fail"} {
		for _, suffix := range []string{"", "/native"} {
			t.Run(action+suffix, func(t *testing.T) {
				output := []byte(fmt.Sprintf(`{"Action":"run","Test":"TestRuntimeRestoresOnEOFAndClosesOnce"}
{"Action":%q,"Test":%q}
`, action, "TestRuntimeRestoresOnEOFAndClosesOnce"+suffix))
				if _, err := selectedTestPasses(output); err == nil {
					t.Fatal("accepted skipped or failed selected test")
				}
			})
		}
	}
}

func TestSelectedTestsDoNotCountStartsAsPasses(t *testing.T) {
	seen, err := selectedTestPasses([]byte(`{"Action":"run","Test":"TestRuntimeRestoresOnEOFAndClosesOnce"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 0 {
		t.Fatal("counted incomplete test as passed")
	}
}

func TestGoExecutableReportsMissingLookup(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := goExecutable(); err == nil || !strings.Contains(err.Error(), "locate Go executable") {
		t.Fatalf("missing Go executable error = %v", err)
	}
}

func TestGoExecutableRejectsRelativePathResolution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relative executable lookup is platform specific")
	}
	directory := t.TempDir()
	bin := filepath.Join(directory, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(workingDirectory); err != nil {
			t.Error(err)
		}
	}()
	t.Setenv("PATH", "bin")
	t.Setenv("GODEBUG", "execerrdot=0")
	if _, err := goExecutable(); err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("relative Go path was not rejected: %v", err)
	}
}
