// Command stave-dual-runtime runs the local-checkout dual-runtime tutorial.
package main

import (
	"context"
	"flag"
	"os"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/examples/dualruntime"
	"github.com/ben-ranford/stave/runtime/agent"
	"github.com/ben-ranford/stave/runtime/human"
)

func main() {
	agentMode := flag.Bool("agent", false, "serve JSONL agent requests on stdin/stdout")
	flag.Parse()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if *agentMode {
		app, err := dualruntime.New(ctx, dualruntime.AgentManifest())
		if err != nil {
			panic(err)
		}
		defer app.Close()
		opts, err := app.AgentOptions(agent.Options{CompatibilityMode: true})
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
	manifest, err := driver.Open(ctx, capability.Policy{})
	if err != nil {
		panic(err)
	}
	app, err := dualruntime.New(ctx, manifest)
	if err != nil {
		panic(err)
	}
	defer app.Close()
	runtime, err := human.New(app.HumanOptions(driver))
	if err != nil {
		panic(err)
	}
	if err := runtime.Run(ctx); err != nil {
		panic(err)
	}
}
