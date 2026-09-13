package main

import (
	"fmt"
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
