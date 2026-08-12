// Package conformance provides reusable contract checks for primitive trees and
// adapters. It is deliberately renderer-independent and safe for CI/headless use.
package conformance

import (
	"fmt"
	"strings"

	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/keymap"
	"github.com/ben-ranford/stave/semantic"
)

type Failure struct{ Path, Rule, Detail string }

func (f Failure) Error() string { return fmt.Sprintf("%s: %s (%s)", f.Path, f.Rule, f.Detail) }

// ValidateTree enforces the cross-client accessibility/agent parity contract.
func ValidateTree(root semantic.Node) []Failure {
	var out []Failure
	seen := map[semantic.NodeID]bool{}
	var walk func(semantic.Node, string)
	walk = func(n semantic.Node, path string) {
		path += "/" + string(n.Role()) + "[" + n.ID().String() + "]"
		if seen[n.ID()] {
			out = append(out, Failure{path, "unique-node-id", "duplicate node ID"})
		}
		seen[n.ID()] = true
		if n.Flags().Focusable && strings.TrimSpace(n.Name()) == "" {
			out = append(out, Failure{path, "accessible-name", "focusable node has no name"})
		}
		if n.Flags().Focusable && !n.Flags().Visible {
			out = append(out, Failure{path, "focus-state", "hidden node cannot be focusable"})
		}
		if n.Flags().Focusable && n.Flags().Disabled {
			out = append(out, Failure{path, "focus-state", "disabled node cannot be focusable"})
		}
		if interactiveRole(n.Role()) && len(n.Actions()) == 0 {
			out = append(out, Failure{path, "action-parity", "interactive node has no typed action"})
		}
		if n.Flags().Sensitive && n.Value().Text != "" {
			out = append(out, Failure{path, "secret-redaction", "sensitive value contains text"})
		}
		if n.Role() == "table" {
			validateTable(n, path, &out)
		}
		if fieldRole(n.Role()) && n.Metadata()["invalid"] == "true" && !hasRelation(n, "error-message") {
			out = append(out, Failure{path, "field-error-relation", "invalid field lacks error-message relation"})
		}
		if n.Role() == "form" {
			validateForm(n, path, &out)
		}
		if n.Role() == "progressbar" && n.Metadata()["indeterminate"] != "true" && n.Metadata()["total"] == "" {
			out = append(out, Failure{path, "progress-value", "determinate progress lacks total"})
		}
		if n.Role() == "dialog" && len(n.Actions()) == 0 {
			out = append(out, Failure{path, "dialog-close", "dialog has no close/cancel action"})
		}
		if n.Role() == "dialog" {
			validateDialog(n, path, &out)
		}
		if n.Metadata()["recordFallback"] == "disclosure" && n.Role() == "group" {
			validateMasterDetail(n, path, &out)
		}
		if n.Metadata()["recordFallback"] != "" && n.Metadata()["recordFallback"] != "disclosure" {
			out = append(out, Failure{path, "record-fallback", "unsupported record fallback"})
		}
		for _, c := range n.Children() {
			walk(c, path)
		}
	}
	walk(root, "")
	return out
}

// ActionManifest binds semantic action references to keyboard-capable handlers.
// It prevents an adapter from exposing an agent-only action with no human path.
type ActionManifest map[semantic.ActionID]string

func ValidateTreeWithManifest(root semantic.Node, manifest ActionManifest) []Failure {
	failures := ValidateTree(root)
	var walk func(semantic.Node)
	walk = func(n semantic.Node) {
		for _, a := range n.Actions() {
			binding, ok := manifest[a.ID]
			if !ok || strings.TrimSpace(binding) == "" {
				failures = append(failures, Failure{n.ID().String(), "action-binding", string(a.ID)})
			}
		}
		for _, c := range n.Children() {
			walk(c)
		}
	}
	walk(root)
	return failures
}

func interactiveRole(role semantic.Role) bool {
	switch role {
	case "button", "link", "checkbox", "radio", "textbox", "searchbox", "combobox", "option", "menuitem", "tab", "dialog":
		return true
	}
	return false
}
func validateTable(n semantic.Node, path string, out *[]Failure) {
	children := n.Children()
	if len(children) != 2 {
		*out = append(*out, Failure{path, "table-shape", "expected header and body row groups"})
		return
	}
	headerIDs := map[semantic.NodeID]bool{}
	for _, h := range children[0].Children() {
		if h.Role() != "columnheader" {
			*out = append(*out, Failure{path, "table-header", "header child is not columnheader"})
			continue
		}
		headerIDs[h.ID()] = true
		if !hasActionID(h, semantic.ActionID("stave.primitive.core.sort_column.v1")) {
			*out = append(*out, Failure{path, "table-sort", "columnheader lacks sort action"})
		}
	}
	for _, row := range children[1].Children() {
		if row.Role() != "row" {
			*out = append(*out, Failure{path, "table-row", "body child is not row"})
			continue
		}
		if row.Metadata()["rowKey"] == "" {
			*out = append(*out, Failure{path, "stable-row-key", "row key missing"})
		}
		for _, cell := range row.Children() {
			if cell.Role() != "cell" {
				*out = append(*out, Failure{path, "table-cell", "row child is not cell"})
				continue
			}
			if !targetsKnownRelation(cell, "labelled-by", headerIDs) {
				*out = append(*out, Failure{path, "table-cell-header", "cell lacks labelled-by column header relation"})
			}
		}
	}
}

type Adapter interface {
	Name() string
	Render(semantic.Node, Mode) (string, error)
}

// Client is the application-neutral conformance boundary for a Stave adopter.
// It couples semantic rendering to the same typed action registry and keymap
// authority used by the application's human and agent paths.
type Client interface {
	Adapter
	ActionRegistry() *action.Registry
	Keymap() keymap.Map
}

// ClientFixture is one named semantic scenario exercised across a set of
// capability modes. Applications may supply as many fixtures as their domain
// requires without introducing application types into Stave core packages.
type ClientFixture struct {
	Name  string
	Tree  semantic.Node
	Modes []Mode
}

// ClientReport aggregates fixture reports for one application integration.
type ClientReport struct {
	Client   string
	Fixtures []Report
	Failures []Failure
}

type Mode struct {
	Width, Height                                   int
	Color, Unicode, Interactive, TTY, ReducedMotion bool
}
type Report struct {
	Adapter  string
	Modes    int
	Failures []Failure
}

// CheckClient verifies a complete application integration rather than a single
// renderer invocation. It is the preferred conformance entry point for new
// clients; CheckAdapter remains available for focused adapter checks.
func CheckClient(client Client, fixtures []ClientFixture) ClientReport {
	report := ClientReport{Client: client.Name(), Fixtures: make([]Report, 0, len(fixtures))}
	if len(fixtures) == 0 {
		report.Failures = append(report.Failures, Failure{Path: client.Name(), Rule: "client-fixtures", Detail: "client defines no conformance fixtures"})
		return report
	}
	for _, fixture := range fixtures {
		if strings.TrimSpace(fixture.Name) == "" {
			report.Failures = append(report.Failures, Failure{Path: client.Name(), Rule: "fixture-name", Detail: "fixture name must not be blank"})
			continue
		}
		if len(fixture.Modes) == 0 {
			report.Failures = append(report.Failures, Failure{Path: client.Name() + "/" + fixture.Name, Rule: "fixture-modes", Detail: "fixture defines no capability modes"})
			continue
		}
		fixtureReport := CheckAdapter(client, fixture.Tree, fixture.Modes)
		for i := range fixtureReport.Failures {
			fixtureReport.Failures[i].Path = fixture.Name + fixtureReport.Failures[i].Path
		}
		report.Fixtures = append(report.Fixtures, fixtureReport)
		report.Failures = append(report.Failures, fixtureReport.Failures...)
	}
	return report
}

func CheckAdapter(a Adapter, tree semantic.Node, modes []Mode) Report {
	r := Report{Adapter: a.Name(), Modes: len(modes)}
	r.Failures = append(r.Failures, ValidateTree(tree)...)
	if authority, ok := a.(interface {
		ActionRegistry() *action.Registry
		Keymap() keymap.Map
	}); ok {
		r.Failures = append(r.Failures, validateAuthority(tree, authority.ActionRegistry(), authority.Keymap())...)
	} else {
		r.Failures = append(r.Failures, Failure{Path: a.Name(), Rule: "action-authority", Detail: "adapter must expose production action registry and keymap"})
	}
	for _, mode := range modes {
		if _, err := a.Render(tree, mode); err != nil {
			r.Failures = append(r.Failures, Failure{a.Name(), "render", err.Error()})
		}
	}
	return r
}

func fieldRole(role semantic.Role) bool {
	switch role {
	case "textbox", "combobox", "checkbox", "radio", "radiogroup":
		return true
	}
	return false
}

func hasRelation(n semantic.Node, kind semantic.RelationKind) bool {
	for _, relation := range n.Relations() {
		if relation.Kind == kind {
			return true
		}
	}
	return false
}

func hasActionID(n semantic.Node, id semantic.ActionID) bool {
	for _, actionRef := range n.Actions() {
		if actionRef.ID == id {
			return true
		}
	}
	return false
}

func targetsKnownRelation(n semantic.Node, kind semantic.RelationKind, allowed map[semantic.NodeID]bool) bool {
	for _, relation := range n.Relations() {
		if relation.Kind == kind && allowed[relation.Target] {
			return true
		}
	}
	return false
}

func validateForm(n semantic.Node, path string, out *[]Failure) {
	invalidCount := 0
	for _, child := range n.Children() {
		if child.Metadata()["invalid"] == "true" {
			invalidCount++
		}
	}
	if n.Metadata()["invalidCount"] != fmt.Sprintf("%d", invalidCount) {
		*out = append(*out, Failure{path, "form-invalid-count", "form invalidCount metadata is inconsistent"})
	}
}

func validateDialog(n semantic.Node, path string, out *[]Failure) {
	if n.Metadata()["modal"] == "true" {
		if !hasActionID(n, semantic.ActionID("stave.primitive.core.close_modal.v1")) || !hasActionID(n, semantic.ActionID("stave.primitive.core.cancel.v1")) {
			*out = append(*out, Failure{path, "modal-dismiss", "modal lacks close and cancel actions"})
		}
		if n.Metadata()["focusRestore"] == "" {
			*out = append(*out, Failure{path, "modal-focus-restore", "modal lacks focus restoration metadata"})
		}
	}
}

func validateMasterDetail(n semantic.Node, path string, out *[]Failure) {
	if !hasActionID(n, semantic.ActionID("stave.primitive.core.open_detail.v1")) || !hasActionID(n, semantic.ActionID("stave.primitive.core.back_to_master.v1")) {
		*out = append(*out, Failure{path, "master-detail-actions", "master-detail contract missing explicit actions"})
	}
	if n.Metadata()["focusRestore"] == "" {
		*out = append(*out, Failure{path, "master-detail-focus-restore", "master-detail lacks focus restoration metadata"})
	}
}

func validateAuthority(tree semantic.Node, registry *action.Registry, km keymap.Map) []Failure {
	if registry == nil {
		return []Failure{{Path: "adapter", Rule: "action-registry", Detail: "adapter returned nil action registry"}}
	}
	manifest := registry.Manifest()
	manifestIDs := make(map[action.ID]bool, len(manifest))
	for _, definition := range manifest {
		manifestIDs[definition.ID] = true
	}
	bindings := make(map[action.ID]bool, len(km.Inventory()))
	for _, item := range km.Inventory() {
		if item.RouteKind == keymap.RouteAction {
			bindings[item.ActionID] = true
			if !manifestIDs[item.ActionID] {
				return []Failure{{Path: "adapter", Rule: "keymap-action-authority", Detail: string(item.ActionID)}}
			}
		}
	}
	var failures []Failure
	var walk func(semantic.Node)
	walk = func(n semantic.Node) {
		for _, ref := range n.Actions() {
			id := action.ID(ref.ID)
			if !manifestIDs[id] {
				failures = append(failures, Failure{Path: n.ID().String(), Rule: "action-registry-authority", Detail: string(ref.ID)})
			}
			if !bindings[id] {
				failures = append(failures, Failure{Path: n.ID().String(), Rule: "keymap-binding-authority", Detail: string(ref.ID)})
			}
		}
		for _, child := range n.Children() {
			walk(child)
		}
	}
	walk(tree)
	return failures
}
