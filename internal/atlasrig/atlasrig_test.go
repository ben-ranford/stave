package atlasrig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ben-ranford/stave"
	"github.com/ben-ranford/stave/action"
	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/conformance"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/primitive"
	"github.com/ben-ranford/stave/replay"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/state"
)

func TestManifestIsStrictAndCheckedIn(t *testing.T) {
	encoded, err := ManifestJSON(DefaultManifest())
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseManifest(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed, DefaultManifest()) {
		t.Fatalf("manifest round trip changed:\n%+v\n%+v", parsed, DefaultManifest())
	}

	fixture, err := os.ReadFile("../../testdata/atlas-scenarios.json")
	if err != nil {
		t.Fatal(err)
	}
	checkedIn, err := ParseManifest(fixture)
	if err != nil {
		t.Fatalf("checked-in manifest is invalid: %v", err)
	}
	if !reflect.DeepEqual(checkedIn, DefaultManifest()) {
		t.Fatal("checked-in Atlas manifest is stale")
	}

	for name, hostile := range map[string]string{
		"empty":            ``,
		"array":            `[]`,
		"unknown":          `{"schemaVersion":"atlas-proof-rig/v1","scenarios":[],"profiles":[],"unknown":true}`,
		"trailing":         string(encoded) + `{}`,
		"root duplicate":   `{"schemaVersion":"atlas-proof-rig/v1","schemaVersion":"atlas-proof-rig/v1","scenarios":[],"profiles":[]}`,
		"nested duplicate": `{"schemaVersion":"atlas-proof-rig/v1","scenarios":[{"name":"ready","name":"other","description":"x","tier":"core"}],"profiles":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(hostile)); err == nil {
				t.Fatalf("hostile manifest %q succeeded", hostile)
			}
		})
	}
}

func TestProofMatrixIsCompleteDeterministicAndProfileHonest(t *testing.T) {
	rig := mustRig(t)
	first, err := rig.Matrix(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := rig.Matrix(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatal("identical Atlas proof matrices differ")
	}
	want := len(rig.Manifest().Scenarios) * len(rig.Manifest().Profiles)
	if len(first.Artifacts) != want {
		t.Fatalf("artifacts=%d want=%d", len(first.Artifacts), want)
	}
	seen := map[string]Artifact{}
	for _, artifact := range first.Artifacts {
		key := artifact.Scenario + "/" + artifact.Profile
		if _, ok := seen[key]; ok {
			t.Fatalf("duplicate artifact %s", key)
		}
		seen[key] = artifact
		if !slices.Equal(artifact.ActionIDs, artifact.KeymapActionIDs) {
			t.Fatalf("%s action/keymap parity differs", key)
		}
	}
	for _, profile := range []string{"truecolor", "ansi256", "ansi16", "monochrome"} {
		if !seen["ready/"+profile].ContainsANSI {
			t.Fatalf("profile %s did not emit terminal styling", profile)
		}
	}
	for _, profile := range []string{"machine-json", "ascii-narrow", "non-tty", "accessible"} {
		if seen["ready/"+profile].ContainsANSI {
			t.Fatalf("profile %s emitted terminal control sequences", profile)
		}
	}
	if got := seen["ready/machine-json"].OutputMode; got != capability.OutputMachineJSON {
		t.Fatalf("machine profile output mode=%s", got)
	}
	if !seen["ready/accessible"].ScreenReader || !seen["ready/accessible"].ReducedMotion {
		t.Fatal("accessible profile omitted screen-reader/reduced-motion semantics")
	}
	if !seen["ready/reduced-motion"].ReducedMotion {
		t.Fatal("reduced-motion profile lost its capability")
	}
	if !seen["ready/ascii-narrow"].PlainASCII || seen["ready/ascii-narrow"].Viewport.Width != 28 {
		t.Fatal("narrow ASCII profile is not honest")
	}
	if seen["ready/truecolor"].TerminalHash == seen["ready/ansi256"].TerminalHash || seen["ready/ansi256"].TerminalHash == seen["ready/ansi16"].TerminalHash {
		t.Fatal("colour ladder collapsed distinct terminal encodings")
	}
}

func TestEveryScenarioHasDistinctValidSemanticsAndConforms(t *testing.T) {
	rig := mustRig(t)
	hashes := map[string]string{}
	markers := map[string]string{
		"ready": "Route matrix", "empty": "No routes match", "loading": "Loading routes",
		"error": "Unable to load routes", "invalid-form": "Route ID is required",
		"modal-confirmation": "Retire north gate", "dense-table": "Queue depth by route",
	}
	for _, scenario := range rig.Manifest().Scenarios {
		root, err := rig.Tree(scenario.Name)
		if err != nil {
			t.Fatal(err)
		}
		tree, err := semantic.NewTree(1, root)
		if err != nil {
			t.Fatalf("%s: %v", scenario.Name, err)
		}
		if err := tree.Snapshot().Validate(); err != nil {
			t.Fatalf("%s snapshot: %v", scenario.Name, err)
		}
		if previous, exists := hashes[tree.Hash()]; exists {
			t.Fatalf("scenarios %s and %s share semantic hash", previous, scenario.Name)
		}
		hashes[tree.Hash()] = scenario.Name
		artifact, err := rig.Prepare(context.Background(), scenario.Name, "non-tty")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(artifact.Plain, markers[scenario.Name]) {
			t.Fatalf("scenario %s missing semantic marker %q", scenario.Name, markers[scenario.Name])
		}
	}
	if err := rig.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorityRejectsUndeclaredScenarioAction(t *testing.T) {
	rig := mustRig(t)
	root, err := primitive.Button(primitive.Options{
		Namespace: "atlas", View: "hostile", Entity: "unplanned",
		Actions: []semantic.ActionRef{{ID: "stave.atlas.unplanned.v1", Label: "Unplanned", Default: true}},
	}, "Unplanned")
	if err != nil {
		t.Fatal(err)
	}
	report := conformance.CheckClient(rig, []conformance.ClientFixture{{
		Name: "unplanned-action", Tree: root,
		Modes: []conformance.Mode{{Width: 80, Height: 24}},
	}})
	var registryFailure, keymapFailure bool
	for _, failure := range report.Failures {
		registryFailure = registryFailure || failure.Rule == "action-registry-authority"
		keymapFailure = keymapFailure || failure.Rule == "keymap-binding-authority"
	}
	if !registryFailure || !keymapFailure {
		t.Fatalf("undeclared action escaped independent authority: %+v", report.Failures)
	}
}

func TestScenarioTransitionProducesValidSemanticPatch(t *testing.T) {
	rig := mustRig(t)
	readyRoot, err := rig.Tree("ready")
	if err != nil {
		t.Fatal(err)
	}
	emptyRoot, err := rig.Tree("empty")
	if err != nil {
		t.Fatal(err)
	}
	ready, err := semantic.NewTree(1, readyRoot)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := semantic.NewTree(2, emptyRoot)
	if err != nil {
		t.Fatal(err)
	}
	patch := semantic.Diff(ready, empty)
	if err := patch.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(patch.Added) == 0 || len(patch.Removed) == 0 {
		t.Fatalf("scenario transition patch=%+v", patch)
	}
}

func TestProgramOwnsStateAndTranscriptReplays(t *testing.T) {
	rig := mustRig(t)
	first := mustSession(t, rig, "ready", "non-tty", "atlas-replay")
	incoming, err := event.New(event.Key, event.KeyPayload{Key: "enter"})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Session.Send(incoming); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := first.Session.Wait(ctx, func(snapshot state.State[Model]) bool { return snapshot.Sequence == 1 && snapshot.Model.Events == 1 }); err != nil {
		t.Fatal(err)
	}
	transcript, err := first.Session.Transcript()
	if err != nil {
		t.Fatal(err)
	}
	if len(transcript.Records) != 1 {
		t.Fatalf("records=%d", len(transcript.Records))
	}

	second := mustSession(t, rig, "ready", "non-tty", "atlas-replay")
	actual, err := replay.Execute(ctx, transcript, func(ctx context.Context, prior state.Checkpoint, recorded event.Event) (state.Checkpoint, error) {
		checkpoint, err := second.Session.Checkpoint()
		if err != nil {
			return state.Checkpoint{}, err
		}
		if checkpoint.Checksum != prior.Checksum {
			return state.Checkpoint{}, fmt.Errorf("Atlas replay prior checkpoint mismatch")
		}
		if err := second.Session.Send(recorded); err != nil {
			return state.Checkpoint{}, err
		}
		if err := second.Session.Wait(ctx, func(snapshot state.State[Model]) bool { return snapshot.Sequence == recorded.Sequence }); err != nil {
			return state.Checkpoint{}, err
		}
		return second.Session.Checkpoint()
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := replay.Validate(transcript, actual); err != nil {
		t.Fatalf("Atlas replay diverged: %v", err)
	}
}

func TestTypedActionsValidateAndConfirmationIsSingleUse(t *testing.T) {
	rig := mustRig(t)
	registry := rig.ActionRegistry()
	for _, mapping := range rig.Keymap().Bindings() {
		result := registry.Invoke(context.Background(), action.Call{
			ActionID: mapping.Route.ActionID, SessionID: "atlas-keymap", Arguments: mapping.Route.Arguments,
		})
		if mapping.Route.ActionID == RetireAction {
			if result.Error == nil || result.Error.Code != action.ConfirmationRequired {
				t.Fatalf("keymap retire route=%+v", result)
			}
			continue
		}
		if result.Status != action.ResultOK {
			t.Fatalf("keymap route %s=%+v", mapping.Route.ActionID, result)
		}
	}
	inspect := registry.Invoke(context.Background(), action.Call{
		ActionID: InspectAction, SessionID: "atlas-actions", Arguments: json.RawMessage(`{"routeId":"north-gate"}`),
	})
	if inspect.Status != action.ResultOK || string(inspect.Output) != `{"routeId":"north-gate","status":"steady","queueDepth":4}` {
		t.Fatalf("inspect=%+v", inspect)
	}
	invalid := registry.Invoke(context.Background(), action.Call{
		ActionID: InspectAction, SessionID: "atlas-actions", Arguments: json.RawMessage(`{"routeId":"north-gate","unknown":true}`),
	})
	if invalid.Error == nil || invalid.Error.Code != action.InvalidArgument {
		t.Fatalf("invalid inspect=%+v", invalid)
	}

	definition, ok := registry.Definition(RetireAction)
	if !ok || definition.Safety != action.Consequential || !definition.Confirmation.Required || !definition.Confirmation.SingleUse {
		t.Fatalf("retire definition=%+v", definition)
	}
	arguments := json.RawMessage(`{"routeId":"north-gate","reason":"capacity drift"}`)
	confirmation, err := action.NewConfirmation("atlas-actions", definition, semantic.Target{}, arguments, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.IssueConfirmation(confirmation); err != nil {
		t.Fatal(err)
	}
	call := action.Call{ActionID: RetireAction, SessionID: "atlas-actions", Arguments: arguments, Confirmation: &confirmation}
	if result := registry.Invoke(context.Background(), call); result.Status != action.ResultOK {
		t.Fatalf("retire=%+v", result)
	}
	if result := registry.Invoke(context.Background(), call); result.Error == nil || result.Error.Code != action.ConfirmationInvalid {
		t.Fatalf("confirmation replay=%+v", result)
	}
}

func TestUnsupportedSelectionsFailClosedAndOutputIsSanitized(t *testing.T) {
	rig := mustRig(t)
	if _, err := rig.Tree("missing"); err == nil {
		t.Fatal("unsupported scenario succeeded")
	}
	if _, err := rig.Prepare(context.Background(), "ready", "missing"); err == nil {
		t.Fatal("unsupported profile succeeded")
	}
	artifact, err := rig.Prepare(context.Background(), "error", "truecolor")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"Lopper", "\x1b]52", "top-secret"} {
		if strings.Contains(artifact.Plain, forbidden) || strings.Contains(string(artifact.Machine), forbidden) {
			t.Fatalf("artifact contains forbidden value %q", forbidden)
		}
	}
}

func mustRig(t *testing.T) *Rig {
	t.Helper()
	rig, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return rig
}

func mustSession(t *testing.T, rig *Rig, scenario, profile, sessionID string) *stave.Prepared[Model] {
	t.Helper()
	prepared, err := rig.NewSession(context.Background(), scenario, profile, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { prepared.Session.Close() })
	return prepared
}
