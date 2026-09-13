package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ben-ranford/stave/capability"
	"github.com/ben-ranford/stave/event"
	"github.com/ben-ranford/stave/replay"
	"github.com/ben-ranford/stave/semantic"
	"github.com/ben-ranford/stave/state"
)

func TestRunReportsDistinctValidateCompareAndInvalidStatuses(t *testing.T) {
	directory := t.TempDir()
	expectedPath := filepath.Join(directory, "expected.json")
	actualPath := filepath.Join(directory, "actual.json")
	transcript := testTranscript(t)
	writeTranscript(t, expectedPath, transcript)
	writeTranscript(t, actualPath, transcript)

	if code, result := runReport(t, "validate", "-input", expectedPath); code != exitSuccess || result.Status != "valid" {
		t.Fatalf("validate code=%d report=%+v", code, result)
	}
	if code, result := runReport(t, "compare", "-expected", expectedPath, "-actual", actualPath); code != exitSuccess || result.Status != "match" {
		t.Fatalf("compare code=%d report=%+v", code, result)
	}
	transcript.Records[0].Result.Hashes.Model = "different"
	transcript.Records[0].Result.Revision++
	transcript.Records[0].Event.Revision = transcript.Records[0].Result.Revision
	if err := replay.ValidateTranscript(transcript); err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, actualPath, transcript)
	if code, result := runReport(t, "compare", "-expected", expectedPath, "-actual", actualPath); code != exitMismatch || result.Status != "mismatch" || result.Divergence == nil {
		t.Fatalf("mismatch code=%d report=%+v", code, result)
	}
	if err := os.WriteFile(actualPath, []byte(`{"schemaVersion":"stave.replay/v99"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if code, result := runReport(t, "validate", "-input", actualPath); code != exitInvalid || result.Status != "invalid" {
		t.Fatalf("invalid code=%d report=%+v", code, result)
	}
	if err := os.WriteFile(actualPath, []byte(`{"payload":"do-not-disclose"`), 0600); err != nil {
		t.Fatal(err)
	}
	if code, result := runReport(t, "validate", "-input", actualPath); code != exitInvalid || result.Error != invalidInputMessage {
		t.Fatalf("sensitive invalid input code=%d report=%+v", code, result)
	}
}

type failingReportWriter struct{}

func (failingReportWriter) Write([]byte) (int, error) { return 0, errors.New("private-output-path") }

func TestRunFailsWhenReportCannotBeWritten(t *testing.T) {
	directory := t.TempDir()
	valid, different := filepath.Join(directory, "valid.json"), filepath.Join(directory, "different.json")
	transcript := testTranscript(t)
	writeTranscript(t, valid, transcript)
	transcript.Records[0].Result.Hashes.Model = "different"
	transcript.Records[0].Result.Revision++
	transcript.Records[0].Event.Revision = transcript.Records[0].Result.Revision
	if err := replay.ValidateTranscript(transcript); err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, different, transcript)
	for _, args := range [][]string{
		{"validate", "-input", valid},
		{"compare", "-expected", valid, "-actual", valid},
		{"compare", "-expected", valid, "-actual", different},
		{"validate", "-input", filepath.Join(directory, "missing.json")},
	} {
		var stderr bytes.Buffer
		if code := run(args, failingReportWriter{}, &stderr); code != 1 {
			t.Errorf("run(%q) output failure code=%d, want 1", args, code)
		}
		if stderr.String() != "cannot write replay report\n" {
			t.Errorf("unsafe or missing output diagnostic: %q", stderr.String())
		}
	}
}

func runReport(t *testing.T, args ...string) (int, report) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	var result report
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("run(%q) output=%q stderr=%q: %v", args, stdout.String(), stderr.String(), err)
	}
	return code, result
}

func writeTranscript(t *testing.T, path string, transcript replay.Transcript) {
	t.Helper()
	data, err := transcript.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func testTranscript(t *testing.T) replay.Transcript {
	t.Helper()
	id, err := semantic.NodeIDFor(semantic.NodeKey{AppNamespace: "stave", View: "test", Kind: "root", Entity: "main", Slot: "body"})
	if err != nil {
		t.Fatal(err)
	}
	node, err := semantic.NewNode(semantic.NodeSpec{ID: id, Generation: 1, Role: "application", Name: "root", Flags: semantic.Flags{Visible: true}})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := semantic.NewTree(1, node)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.New("session-1", map[string]any{"status": "ok"}, tree, state.Meta{Sequence: 1, Revision: 1, Capabilities: capability.Manifest{Width: 80, Height: 24}, ConfigHash: "cfg", ThemeHash: "theme", SurfaceHash: "surface"}, state.ModelPolicy[map[string]any]{})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := snapshot.Checkpoint(state.ModelPolicy[map[string]any]{})
	if err != nil {
		t.Fatal(err)
	}
	transcript := replay.NewTranscript(snapshot.SessionID, snapshot.Versions, checkpoint)
	eventValue, err := event.New(event.Key, event.KeyPayload{Key: "enter"})
	if err != nil {
		t.Fatal(err)
	}
	transcript.Append(replay.Record{Event: eventValue.WithAccepted(2, 1), Prior: replay.DigestFromState(snapshot), Result: replay.DigestFromState(snapshot)})
	transcript.Records[0].Result.Sequence = 2
	return transcript
}
