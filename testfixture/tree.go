package testfixture

import (
	"fmt"
	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/semantic"
)

func BrandTree(namespace, status string) (semantic.Node, error) {
	t, err := primitive.Text(primitive.Options{Namespace: namespace, View: "home", Entity: "title", Name: "Title"}, "Example application")
	if err != nil {
		return semantic.Node{}, err
	}
	s, err := primitive.Status(primitive.Options{Namespace: namespace, View: "home", Entity: "status", Name: "Status"}, status)
	if err != nil {
		return semantic.Node{}, err
	}
	return primitive.Stack(primitive.Options{Namespace: namespace, View: "home", Entity: "root", Name: "Application", Metadata: map[string]string{"brand": namespace}}, t, s)
}
func TableTree(namespace string) (semantic.Node, error) {
	return primitive.Table(primitive.TableOptions{Options: primitive.Options{Namespace: namespace, View: "summary", Entity: "dependencies", Name: "Dependencies"}, Columns: []primitive.Column{{Key: "name", Name: "Name"}, {Key: "count", Name: "Count", Numeric: true, Sticky: true}}, Rows: []primitive.TableRow{{Key: "a", Name: "A", Cells: []string{"A", "2"}, Selected: true}, {Key: "b", Name: "B", Cells: []string{"B", "10"}}}, Total: 2})
}
func Must(n semantic.Node, err error) semantic.Node {
	if err != nil {
		panic(fmt.Sprintf("fixture: %v", err))
	}
	return n
}
