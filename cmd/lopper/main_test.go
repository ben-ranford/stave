package main

import (
	"context"
	"strings"
	"testing"

	"github.com/ben-ranford/stave"
	"github.com/ben-ranford/stave/capability"
)

func TestLopperFixtureUsesIndependentTerminalProfile(t *testing.T) {
	t.Parallel()
	prepared, err := lopperProgram().NewSession(context.Background(), stave.SessionOptions{
		SessionID:       "lopper-fixture-test",
		RuntimeDetected: lopperCapabilities(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { prepared.Session.Close() })
	first, err := renderLopper(prepared)
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderLopper(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if first.Terminal != second.Terminal || first.Plain != second.Plain {
		t.Fatal("lopper fixture changed across identical inputs")
	}
	if !strings.Contains(first.Plain, "Dependency summary") || !strings.Contains(first.Plain, "7 dependencies") {
		t.Fatalf("lopper fixture output is incomplete: %q", first.Plain)
	}
	if strings.Contains(first.Plain, "Atlas") || strings.Contains(first.Terminal, "ATLAS") {
		t.Fatalf("lopper fixture depends on Atlas wording: plain=%q terminal=%q", first.Plain, first.Terminal)
	}
	if prepared.Capabilities.OutputMode == capability.OutputMachineJSON || prepared.Capabilities.Color != capability.ColorTrueColor || !prepared.Capabilities.Interactive || !prepared.Capabilities.TTY {
		t.Fatalf("lopper fixture did not retain its full-colour terminal profile: %+v", prepared.Capabilities)
	}
}

func TestTerminalFixtureDropsOnlyTrailingViewportRows(t *testing.T) {
	got := terminalFixture("row padded  \x1b[0m\n    \n    \n")
	if got != "row padded  \x1b[0m\n" {
		t.Fatalf("terminal fixture=%q", got)
	}
}
