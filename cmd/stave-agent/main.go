// Command stave-agent exposes the bounded Stave v1 JSONL agent transport.
package main

import (
	"context"
	"log"
	"os"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/protocol"
	"github.com/ben-ranford/stave/runtime/agent"
)

func main() {
	// log.Default writes stderr, preserving machine stdout purity.
	server := agent.New(agent.Options{
		ServerName:  "stave-agent",
		Application: protocol.Application{ID: "stave-agent", Version: "development"},
		Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
			// The standalone shell has no application session. It therefore
			// negotiates the safe machine-only floor and never invents terminal,
			// action, snapshot, or secure-input capabilities.
			return capability.Manifest{
				ProtocolVersions: []string{protocol.Version},
				OutputMode:       capability.OutputMachineJSONL,
				Color:            capability.ColorNone,
				Unicode:          capability.UnicodeNone,
			}, nil
		},
	})
	if err := server.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		log.Printf("stave-agent stopped: %v", err)
		os.Exit(1)
	}
}
