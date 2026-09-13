package main

import "testing"

func TestSelectedTestStartsCountsExpectedRootTests(t *testing.T) {
	output := []byte(`{"Action":"run","Test":"TestRuntimeRestoresOnEOFAndClosesOnce"}
{"Action":"run","Test":"TestCrossPlatformTerminalColourDetection/linux_truecolor"}
{"Action":"run","Test":"TestCrossPlatformTerminalColourDetection"}
`)
	seen, err := selectedTestStarts(output)
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
