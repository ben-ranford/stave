package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var expected = map[string]bool{
	"TestRuntimeRestoresOnEOFAndClosesOnce":                 true,
	"TestLineDriverEmitsCanonicalTextAndShutdown":           true,
	"TestLineDriverDrawUsesSafePlainWriter":                 true,
	"TestLineDriverDoesNotInventUnsupportedTTYCapabilities": true,
	"TestServeCancellationInterruptsBlockingReader":         true,
	"TestCapabilityEnumsRejectInvalidWireValues":            true,
	"TestDetectExplicitEnvironment":                         true,
	"TestNonTTYMachineOutputIsNotReclassifiedAsPlain":       true,
	"TestNonTTYAccessibleOutputIsNotReclassifiedAsPlain":    true,
	"TestCrossPlatformTerminalColourDetection":              true,
}

const selectedTests = "^(TestRuntimeRestoresOnEOFAndClosesOnce|TestLineDriverEmitsCanonicalTextAndShutdown|TestLineDriverDrawUsesSafePlainWriter|TestLineDriverDoesNotInventUnsupportedTTYCapabilities|TestServeCancellationInterruptsBlockingReader|TestCapabilityEnumsRejectInvalidWireValues|TestDetectExplicitEnvironment|TestNonTTYMachineOutputIsNotReclassifiedAsPlain|TestNonTTYAccessibleOutputIsNotReclassifiedAsPlain|TestCrossPlatformTerminalColourDetection)$"

type testEvent struct {
	Action string
	Test   string
}

func main() {
	command := exec.Command("go", "test", "-json", "-count=1", "-run", selectedTests, "./runtime/human", "./runtime/agent", "./capability")
	output, err := command.Output()
	os.Stdout.Write(output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "native smoke tests failed: %v\n", err)
		os.Exit(1)
	}
	seen, err := selectedTestPasses(output)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for name := range expected {
		if !seen[name] {
			fmt.Fprintf(os.Stderr, "native smoke has no passing result for %s\n", name)
			os.Exit(1)
		}
	}
	fmt.Printf("native smoke selected and passed %d tests\n", len(expected))
}

func selectedTestPasses(output []byte) (map[string]bool, error) {
	seen := make(map[string]bool, len(expected))
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var event testEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("decode go test event: %w", err)
		}
		root, _, _ := strings.Cut(event.Test, "/")
		if !expected[root] {
			continue
		}
		if event.Action == "skip" || event.Action == "fail" {
			return nil, fmt.Errorf("native smoke %s reported %s", event.Test, event.Action)
		}
		if event.Action == "pass" && expected[event.Test] {
			seen[event.Test] = true
		}
	}
	return seen, nil
}
