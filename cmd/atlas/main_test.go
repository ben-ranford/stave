package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ben-ranford/stave/internal/atlasrig"
)

func TestAtlasDefaultRendersMachineFixture(t *testing.T) {
	var output bytes.Buffer
	if err := run(context.Background(), nil, &output); err != nil {
		t.Fatal(err)
	}
	var machine struct {
		SchemaVersion string `json:"schemaVersion"`
		TreeHash      string `json:"treeHash"`
		Plain         string `json:"plain"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &machine); err != nil {
		t.Fatalf("default output is not machine JSON: %v", err)
	}
	if machine.SchemaVersion == "" || machine.TreeHash == "" || !strings.Contains(machine.Plain, "Atlas control deck") {
		t.Fatalf("incomplete default Atlas fixture: %+v", machine)
	}
}

func TestAtlasMatrixAndManifestCommands(t *testing.T) {
	var manifestOutput bytes.Buffer
	if err := run(context.Background(), []string{"-manifest"}, &manifestOutput); err != nil {
		t.Fatal(err)
	}
	manifest, err := atlasrig.ParseManifest(manifestOutput.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	var matrixOutput bytes.Buffer
	if err := run(context.Background(), []string{"-matrix"}, &matrixOutput); err != nil {
		t.Fatal(err)
	}
	var matrix atlasrig.Matrix
	if err := json.Unmarshal(matrixOutput.Bytes(), &matrix); err != nil {
		t.Fatal(err)
	}
	want := len(manifest.Scenarios) * len(manifest.Profiles)
	if matrix.SchemaVersion != atlasrig.ManifestSchemaVersion || len(matrix.Artifacts) != want {
		t.Fatalf("matrix=%+v want artifacts=%d", matrix, want)
	}
}

func TestAtlasVerifyAndSelectionFailures(t *testing.T) {
	var output bytes.Buffer
	if err := run(context.Background(), []string{"-verify"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "atlas proof rig: ok") {
		t.Fatalf("verify output=%q", output.String())
	}
	for _, args := range [][]string{
		{"-scenario", "missing"},
		{"-profile", "missing"},
		{"-format", "missing"},
		{"-verify", "-matrix"},
	} {
		if err := run(context.Background(), args, &bytes.Buffer{}); err == nil {
			t.Fatalf("args %v unexpectedly succeeded", args)
		}
	}
}
