package requirements

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type matrix struct {
	SchemaVersion     string  `json:"schemaVersion"`
	ValidationCommand string  `json:"validationCommand"`
	Entries           []entry `json:"entries"`
}

type entry struct {
	ID                    string   `json:"id"`
	Category              string   `json:"category"`
	Title                 string   `json:"title"`
	SourceRef             string   `json:"sourceRef"`
	Priority              string   `json:"priority,omitempty"`
	PlannedPackageAPI     []string `json:"plannedPackageAPI"`
	VerificationArtifacts []string `json:"verificationArtifacts"`
	DocumentationEvidence []string `json:"documentationEvidence"`
	Status                string   `json:"status"`
	Milestone             string   `json:"milestone"`
	Notes                 string   `json:"notes,omitempty"`
}

func TestTraceabilityMatrix(t *testing.T) {
	matrixPath := filepath.Join(".", "traceability.json")
	raw, err := os.ReadFile(matrixPath)
	if err != nil {
		t.Fatalf("read %s: %v", matrixPath, err)
	}

	var got matrix
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode %s: %v", matrixPath, err)
	}

	if got.SchemaVersion != "stave-traceability-v1" {
		t.Fatalf("unexpected schemaVersion %q", got.SchemaVersion)
	}
	if strings.TrimSpace(got.ValidationCommand) == "" {
		t.Fatal("validationCommand must not be blank")
	}

	expected := map[string][]string{
		"functional_requirement": {
			"FR-001", "FR-002", "FR-003", "FR-004", "FR-005",
			"FR-010", "FR-011", "FR-012", "FR-013", "FR-014", "FR-015", "FR-016", "FR-017",
			"FR-020", "FR-021", "FR-022", "FR-023", "FR-024", "FR-025",
			"FR-030", "FR-031", "FR-032", "FR-033", "FR-034", "FR-035",
			"FR-040", "FR-041", "FR-042", "FR-043",
		},
		"non_functional_requirement": {
			"NFR-001", "NFR-002", "NFR-003", "NFR-004", "NFR-005",
			"NFR-006", "NFR-007", "NFR-008", "NFR-009", "NFR-010",
		},
		"primitive": {
			"PRIMITIVE-P0-TEXT-LABEL",
			"PRIMITIVE-P0-STACK-ROW-GRID",
			"PRIMITIVE-P0-SURFACE-FRAME",
			"PRIMITIVE-P0-TABLE-LIST",
			"PRIMITIVE-P0-STATUS-CHIP",
			"PRIMITIVE-P0-FOCUS-INDICATOR",
			"PRIMITIVE-P0-DISCLOSURE",
			"PRIMITIVE-P0-VIEWPORT",
			"PRIMITIVE-P0-INPUT",
			"PRIMITIVE-P0-EMPTY-LOADING-ERROR",
			"PRIMITIVE-P0-TERMINAL-WRITER",
			"PRIMITIVE-P0-SEMANTIC-SNAPSHOT-NODE",
			"PRIMITIVE-P0-ACTION-DESCRIPTOR",
			"PRIMITIVE-P0-DIAGNOSTICS",
			"PRIMITIVE-P1-TABS",
			"PRIMITIVE-P1-PAGINATION",
			"PRIMITIVE-P1-COMMAND-PALETTE",
			"PRIMITIVE-P1-CHART-PRIMITIVES",
			"PRIMITIVE-P1-INSPECTOR-LAYOUT-SLOT",
			"PRIMITIVE-P1-MODAL-CONFIRMATION-SHELL",
			"PRIMITIVE-P1-SSH-TRANSPORT-ADAPTER",
			"PRIMITIVE-P1-BUBBLE-TEA-LIP-GLOSS-ADAPTERS",
			"PRIMITIVE-P1-RICHER-ACCESSIBILITY-AGENT-TRANSPORTS",
		},
		"architecture_invariant": {
			"AI-01", "AI-02", "AI-03", "AI-04", "AI-05",
			"AI-06", "AI-07", "AI-08", "AI-09", "AI-10",
			"AI-11", "AI-12", "AI-13", "AI-14", "AI-15",
		},
		"milestone": {
			"M0", "M1", "M2", "M3", "M4", "M5", "M6", "M7", "M8",
		},
		"acceptance_criterion": {
			"AC-01", "AC-02", "AC-03", "AC-04", "AC-05", "AC-06", "AC-07", "AC-08", "AC-09", "AC-10", "AC-11",
			"AC-12", "AC-13", "AC-14", "AC-15", "AC-16", "AC-17", "AC-18", "AC-19", "AC-20", "AC-21", "AC-22",
		},
	}

	allowedStatus := []string{"implemented", "planned"}
	allowedMilestones := []string{"M0", "M1", "M2", "M3", "M4", "M5", "M6", "M7", "M8"}
	seen := make(map[string]map[string]int, len(expected))

	for _, e := range got.Entries {
		if strings.TrimSpace(e.ID) == "" {
			t.Fatal("entry id must not be blank")
		}
		if strings.TrimSpace(e.Category) == "" {
			t.Fatalf("entry %s category must not be blank", e.ID)
		}
		ids, ok := expected[e.Category]
		if !ok {
			t.Fatalf("entry %s has unknown category %q", e.ID, e.Category)
		}
		if !slices.Contains(ids, e.ID) {
			t.Fatalf("entry %s is not expected in category %s", e.ID, e.Category)
		}
		if strings.TrimSpace(e.Title) == "" {
			t.Fatalf("entry %s title must not be blank", e.ID)
		}
		if strings.TrimSpace(e.SourceRef) == "" {
			t.Fatalf("entry %s sourceRef must not be blank", e.ID)
		}
		requireStrings(t, e.ID, "plannedPackageAPI", e.PlannedPackageAPI)
		requireEvidenceStrings(t, e.ID, "verificationArtifacts", e.VerificationArtifacts)
		requireEvidenceStrings(t, e.ID, "documentationEvidence", e.DocumentationEvidence)
		if !slices.Contains(allowedStatus, e.Status) {
			t.Fatalf("entry %s has invalid status %q", e.ID, e.Status)
		}
		if !slices.Contains(allowedMilestones, e.Milestone) {
			t.Fatalf("entry %s has invalid milestone %q", e.ID, e.Milestone)
		}

		if seen[e.Category] == nil {
			seen[e.Category] = map[string]int{}
		}
		seen[e.Category][e.ID]++
		if seen[e.Category][e.ID] > 1 {
			t.Fatalf("duplicate entry %s in category %s", e.ID, e.Category)
		}
	}

	for category, ids := range expected {
		for _, id := range ids {
			if seen[category][id] != 1 {
				t.Fatalf("missing entry %s in category %s", id, category)
			}
		}
	}

	md, err := os.ReadFile(filepath.Join(".", "TRACEABILITY.md"))
	if err != nil {
		t.Fatalf("read TRACEABILITY.md: %v", err)
	}
	if strings.TrimSpace(string(md)) == "" {
		t.Fatal("TRACEABILITY.md must not be blank")
	}
}

// TestCandidateNormativeEntries allows explicitly planned external proving-
// client work while refusing to label unpublished external evidence as an
// implemented normative requirement. Ordinary pull-request CI runs this gate.
func TestCandidateNormativeEntries(t *testing.T) {
	if os.Getenv("STAVE_CANDIDATE_GATE") != "1" {
		t.Skip("candidate gate is opt-in; set STAVE_CANDIDATE_GATE=1")
	}
	got := readMatrix(t)
	for _, e := range got.Entries {
		if !normativeEntry(e) {
			continue
		}
		if e.Status == "planned" && strings.TrimSpace(e.Notes) == "" {
			t.Errorf("candidate planned normative entry %s lacks an explicit blocker note", e.ID)
		}
		if e.Status == "implemented" && hasUnpublishedEvidence(e) {
			t.Errorf("candidate implemented normative entry %s relies on unpublished external evidence", e.ID)
		}
	}
}

// TestGAReleaseNormativeEntries is intentionally stricter than the candidate
// gate: a GA release may not contain planned normative work or unpublished
// proving-client evidence. Only tag/release automation enables it.
func TestGAReleaseNormativeEntries(t *testing.T) {
	if os.Getenv("STAVE_GA_RELEASE_GATE") != "1" {
		t.Skip("GA release gate is opt-in; set STAVE_GA_RELEASE_GATE=1")
	}
	got := readMatrix(t)
	for _, e := range got.Entries {
		if !normativeEntry(e) {
			continue
		}
		if e.Status == "planned" {
			t.Errorf("GA release contains planned normative entry %s (%s)", e.ID, e.Title)
		}
		if hasUnpublishedEvidence(e) {
			t.Errorf("GA release contains unpublished external evidence for %s", e.ID)
		}
	}
}

func readMatrix(t *testing.T) matrix {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(".", "traceability.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got matrix
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func normativeEntry(e entry) bool {
	return e.Category == "acceptance_criterion" || e.Priority == "P0" || e.Priority == "P1"
}

func hasUnpublishedEvidence(e entry) bool {
	for _, values := range [][]string{e.VerificationArtifacts, e.DocumentationEvidence} {
		for _, value := range values {
			if strings.HasPrefix(value, "external://") && strings.Contains(value, "@unpublished-") {
				return true
			}
		}
	}
	return false
}

func requireStrings(t *testing.T, id, field string, values []string) {
	t.Helper()
	if len(values) == 0 {
		t.Fatalf("entry %s %s must not be empty", id, field)
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("entry %s %s contains blank evidence", id, field)
		}
	}
}

func requireEvidenceStrings(t *testing.T, id, field string, values []string) {
	t.Helper()
	if len(values) == 0 {
		t.Fatalf("entry %s %s must not be empty", id, field)
	}
	for _, value := range values {
		validateEvidenceString(t, id, field, value)
	}
}

func validateEvidenceString(t *testing.T, id, field, value string) {
	t.Helper()
	value = strings.TrimSpace(value)
	if value == "" {
		t.Fatalf("entry %s %s contains blank evidence", id, field)
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, "OMX") {
		t.Fatalf("entry %s %s contains non-portable path evidence %q", id, field, value)
	}
	if strings.HasPrefix(value, ".doc/") || strings.HasPrefix(value, ".artifacts/") {
		t.Fatalf("entry %s %s contains ignored local source evidence %q", id, field, value)
	}
	if strings.Contains(value, "://") && !strings.HasPrefix(value, "external://") {
		t.Fatalf("entry %s %s contains unsupported evidence scheme %q", id, field, value)
	}
	if strings.HasPrefix(value, "external://") {
		validateExternalEvidence(t, id, field, value)
		return
	}
	if isCommandEvidence(value) {
		validateCommandEvidence(t, id, field, value)
		return
	}
	requireRepoLocalPath(t, id, field, value)
}

func validateExternalEvidence(t *testing.T, id, field, value string) {
	t.Helper()
	withoutScheme := strings.TrimPrefix(value, "external://")
	locator, fragment, ok := strings.Cut(withoutScheme, "#")
	if !ok || fragment == "" {
		t.Fatalf("entry %s %s external evidence lacks a path fragment %q", id, field, value)
	}
	repository, revision, ok := strings.Cut(locator, "@")
	if !ok || !strings.HasPrefix(repository, "github.com/") || strings.Count(repository, "/") != 2 || strings.TrimSpace(revision) == "" {
		t.Fatalf("entry %s %s external evidence lacks a GitHub repository and revision %q", id, field, value)
	}
	if len(revision) != 40 && !strings.HasPrefix(revision, "unpublished-") {
		t.Fatalf("entry %s %s external revision must be a commit or explicitly unpublished %q", id, field, revision)
	}
	pathValue := fragment
	if index := strings.Index(pathValue, "::"); index >= 0 {
		if strings.TrimSpace(pathValue[index+2:]) == "" {
			t.Fatalf("entry %s %s external symbol is blank %q", id, field, value)
		}
		pathValue = pathValue[:index]
	}
	if pathValue == "" || strings.HasPrefix(pathValue, "/") || strings.Contains(pathValue, "..") || strings.ContainsAny(pathValue, " \t") {
		t.Fatalf("entry %s %s external path is unsafe %q", id, field, pathValue)
	}
}

func isCommandEvidence(value string) bool {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return false
	}
	if strings.HasPrefix(fields[0], "go") || fields[0] == "make" {
		return true
	}
	return strings.HasPrefix(value, "STAVE_")
}

func validateCommandEvidence(t *testing.T, id, field, value string) {
	t.Helper()
	fields := strings.Fields(value)
	for i, fieldValue := range fields {
		if fieldValue == "go" && i+1 < len(fields) && (fields[i+1] == "run" || fields[i+1] == "test" || fields[i+1] == "build" || fields[i+1] == "vet") {
			for _, arg := range fields[i+2:] {
				if strings.Contains(arg, "...") {
					continue
				}
				if strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") {
					requireRepoLocalPath(t, id, field, arg)
				}
			}
			return
		}
		if fieldValue == "make" {
			return
		}
	}
}

func requireRepoLocalPath(t *testing.T, id, field, value string) {
	t.Helper()
	value = strings.TrimSpace(value)
	symbol := ""
	if i := strings.Index(value, "::"); i >= 0 {
		symbol = strings.TrimSpace(value[i+2:])
		value = value[:i]
	}
	if i := strings.Index(value, "#"); i >= 0 {
		value = value[:i]
	}
	if i := strings.Index(value, " §"); i >= 0 {
		value = value[:i]
	}
	if i := strings.IndexAny(value, " \t"); i >= 0 {
		value = value[:i]
	}
	if value == "" {
		t.Fatalf("entry %s %s contains empty repo-local reference", id, field)
	}
	path := filepath.Join("..", filepath.FromSlash(value))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("entry %s %s references missing repo-local path %q: %v", id, field, value, err)
	}
	if symbol != "" {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("entry %s %s cannot read symbol source %q: %v", id, field, value, err)
		}
		if !strings.Contains(string(contents), "func "+symbol+"(") {
			t.Fatalf("entry %s %s references missing function %s::%s", id, field, value, symbol)
		}
	}
}
