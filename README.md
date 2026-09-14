# Stave

[![CI](https://github.com/ben-ranford/stave/actions/workflows/ci.yml/badge.svg)](https://github.com/ben-ranford/stave/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/ben-ranford/stave.svg)](https://pkg.go.dev/github.com/ben-ranford/stave)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Stave is a Go UI framework for applications that people and agents can use
through the same interface. Build a semantic tree of text, controls, and actions,
then expose it through a terminal UI, plain output, or an agent API.

## Highlights

- **Shared human and agent controls.** Keyboard input and agent requests use the
  same semantic tree and typed action registry.
- **Your application's identity.** Supply your own model, theme, glyphs, labels,
  and keymaps without changing what the UI elements mean.
- **Output for different environments.** Support colour terminals, monochrome,
  ASCII, narrow layouts, and non-interactive output.
- **Repeatable snapshots.** Stable node identities and deterministic encodings
  make UI state available for tests and automation.
- **Go-only core.** The root module has no dependencies outside the standard
  library.

## Overview

Use Stave when a Go application needs both an interactive interface and
structured access for automation. Its primitives describe roles, values, state,
and available actions; renderers turn that description into output. Your
application owns its business logic and effects, while Stave supplies layout,
rendering, sessions, and runtime contracts.

Stave is created by [Ben Ranford](https://github.com/ben-ranford) and is
[MIT licensed](LICENSE).

## Installation

Requires **Go 1.22 or later**. In your Go module, install the documented version:

<!-- v1.0.0-rc.2 x-release-please-version -->
<!-- x-release-please-start-version -->
```sh
go get github.com/ben-ranford/stave@v1.0.0-rc.2
```
<!-- x-release-please-end -->

### Deliberate limits

A stable tag requires published, immutable Lopper proving-client evidence for
parity and rollback before GA promotion. The SSH, Bubble Tea, and Lip Gloss
adapters remain internal until they have independent module versions and tags;
begin with the root module. See the
[compatibility policy](docs/compatibility.md) and [security contract](docs/security.md)
before integrating consequential agent actions.

Development checkouts may contain unreleased changes. Pin a published tag
before deploying consequential agent actions; use the development branch for
evaluation only.

## Quick start

In an empty directory, run `go mod init example.com/hello-stave`, then use the
installation command above. Save the following as `main.go` and run `go run .`.
It creates a text node and reads its role and value from a semantic tree:

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


This example needs no terminal renderer. To add application state, actions, and
human or agent runtimes, continue with the [adoption guide](docs/client-adoption.md).

## Documentation

- [Adoption guide](docs/client-adoption.md): build an application and validate its integration.
- [UI primitives](docs/primitives.md): compose text, tables, forms, progress, and overlays.
- [Accessibility and agent control](docs/accessibility-agent-parity.md): node metadata, keyboard paths, and action policies.
- [Compatibility](docs/compatibility.md): versioned APIs, schemas, and breaking changes.
- [Security](docs/security.md): authority, confirmation, and secret handling.
- [Go reference](https://pkg.go.dev/github.com/ben-ranford/stave): package and API documentation.

## Feedback and contributing

Report bugs or propose features through [GitHub issues](https://github.com/ben-ranford/stave/issues).
Include the Go version, Stave tag or commit, and a small reproduction for bugs.
For security reports, follow [SECURITY.md](SECURITY.md).

Start with [CONTRIBUTING.md](CONTRIBUTING.md) for development prerequisites and
verification commands. See the [support guide](SUPPORT.md) for request routing
and the [release runbook](docs/releasing.md) for maintainer procedures.
