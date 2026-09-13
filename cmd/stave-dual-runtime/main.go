// Command stave-dual-runtime runs the local-checkout dual-runtime tutorial.
package main

import (
	"context"
	"flag"
	"os"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/examples/dualruntime"
	"github.com/ben-ranford/stave/protocol"
	"github.com/ben-ranford/stave/runtime/agent"
	"github.com/ben-ranford/stave/runtime/human"
)

func main() {
	agentMode := flag.Bool("agent", false, "serve JSONL agent requests on stdin/stdout")
	flag.Parse()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app, err := dualruntime.New(ctx)
	if err != nil {
		panic(err)
	}
	defer app.Close()
	if *agentMode {
		opts, err := app.AgentOptions(agent.Options{Actions: app.Registry, CompatibilityMode: true, Authorize: func(context.Context, action.Call) *action.Error { return nil }, Negotiate: func(context.Context, map[string]any) (capability.Manifest, error) {
			return capability.Manifest{ProtocolVersions: []string{protocol.Version}, OutputMode: capability.OutputMachineJSONL, SnapshotModes: []string{"full"}}, nil
		}})
		if err != nil {
			panic(err)
		}
		if err := agent.New(opts).Serve(ctx, os.Stdin, os.Stdout); err != nil {
			panic(err)
		}
		return
	}
	driver, err := human.NewLineDriver(human.LineDriverOptions{Input: os.Stdin, Output: os.Stdout, TTY: false})
	if err != nil {
		panic(err)
	}
	runtime, err := human.New(app.HumanOptions(driver))
	if err != nil {
		panic(err)
	}
	if err := runtime.Run(ctx); err != nil {
		panic(err)
	}
}
