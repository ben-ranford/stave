package stave_test

import (
	"fmt"

	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/semantic"
)

func Example_quickStart() {
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

	// Output:
	// text: Hello, Stave!
}
