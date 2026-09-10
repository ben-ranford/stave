# Stave

Stave is a renderer-independent Go UI primitives framework for branded human and agent interfaces. Applications own their identity (theme tokens, glyphs, assets, semantic labels, actions, and keymaps); Stave owns deterministic state, layout, rendering, protocol, and runtime contracts.

## Release status

`v1.0.0-rc.2` <!-- x-release-please-version --> is the current root-module release candidate. It
is not a GA release: promotion requires published immutable Lopper proving-client
evidence for parity and rollback. The nested SSH, Bubble Tea, and Lip Gloss
adapters remain internal until they receive independent module versions and tags.

`v1.0.0-rc.2` contains the confirmation-security fixes. After its tag is
published, pin it before deploying consequential agent actions; until then,
treat the development branch as evaluation-only source.

## Quick start

Create a module and try the current development source. Go records the resolved
commit as a pseudo-version in `go.mod`:

```sh
go mod init example.com/hello-stave
go get github.com/ben-ranford/stave@main
```

Save this as `main.go`, then run `go run .`:

```go
package main

import (
	"fmt"

	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/semantic"
)

func main() {
	node, err := primitive.Text(primitive.Options{
		Namespace: "hello",
		View:      "main",
		Entity:    "welcome",
		Name:      "Welcome",
	}, "Hello, Stave!")
	if err != nil {
		panic(err)
	}
	tree, err := semantic.NewTree(1, node)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s: %s\n", tree.Root().Role(), tree.Root().Value().Text)
}
```

```text
text: Hello, Stave!
```

## Documentation

Start with the [adoption guide](docs/client-adoption.md), then see the
[primitive reference](docs/primitives.md),
[accessibility and agent-control expectations](docs/accessibility-agent-parity.md),
[compatibility guarantees](docs/compatibility.md), and
[security contract](docs/security.md).
Maintainers should follow the [release runbook](docs/releasing.md).

## Verify

Repository verification has [development prerequisites](CONTRIBUTING.md#development-prerequisites), including the Node runtime configured by CI. Library consumers only need Go for the quick start above.

```sh
go test ./... -count=1
make verify
make schema-freshness
```

Schemas are checked in under [`schema/`](schema/). Changes that affect a wire
or semantic contract require schema review and migration notes.
