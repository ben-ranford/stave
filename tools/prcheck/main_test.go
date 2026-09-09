package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAcceptsCompletedRenderedHTML(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(nil, env(map[string]string{"PR_TITLE": "ci: add metadata check", "PR_BODY": fixture(t, "completed.html")}), &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
}

func TestValidateRejectsInvalidTitleAndEmptyRenderedSections(t *testing.T) {
	if err := validate("bug: wrong type", "bug/42", fixture(t, "completed.html"), identity{}); err == nil || !strings.Contains(err.Error(), "Conventional Commit") {
		t.Fatalf("invalid title error = %v", err)
	}
	if err := validate("ci: add metadata check", "ci/metadata", fixture(t, "empty-links.html"), identity{}); err == nil || !strings.Contains(err.Error(), `section "Summary"`) {
		t.Fatalf("empty links accepted as content: %v", err)
	}
}

func TestValidateRequiresRenderedH2Elements(t *testing.T) {
	err := validate("fix: parser", "fix/parser", fixture(t, "hidden-headings.html"), identity{})
	if err == nil || !strings.Contains(err.Error(), `section "Summary"`) || !strings.Contains(err.Error(), `section "Validation"`) {
		t.Fatalf("comments or preformatted heading text was accepted: %v", err)
	}
}

func TestValidateRejectsEmptyRenderedContainers(t *testing.T) {
	err := validate("fix: parser", "fix/parser", fixture(t, "empty-containers.html"), identity{})
	if err == nil || !strings.Contains(err.Error(), `section "Summary"`) || !strings.Contains(err.Error(), `section "Validation"`) || !strings.Contains(err.Error(), `section "Release Notes"`) {
		t.Fatalf("empty rendered containers were accepted: %v", err)
	}
}

func TestValidateAcceptsNestedHeadingMarkupAndCodeLiteralText(t *testing.T) {
	if err := validate("fix: parser", "fix/parser", fixture(t, "nested-headings-and-code.html"), identity{}); err != nil {
		t.Fatalf("rendered nested heading markup or code content rejected: %v", err)
	}
}

func TestValidateRejectsScriptTextAsInvisible(t *testing.T) {
	body := "<h2>Summary</h2><script>completed</script><h2>Validation</h2><p>completed</p><h2>Release Notes</h2><p>completed</p>"
	if err := validate("fix: parser", "fix/parser", body, identity{}); err == nil || !strings.Contains(err.Error(), `section "Summary"`) {
		t.Fatalf("script text was accepted as visible section content: %v", err)
	}
}

func TestValidateRejectsMalformedRenderedHTML(t *testing.T) {
	if err := validate("fix: parser", "fix/parser", "<h2>Summary</h2><p>&bogus;", identity{}); err == nil || !strings.Contains(err.Error(), "rendered PR body HTML is invalid") {
		t.Fatalf("malformed rendered HTML error = %v", err)
	}
}

func TestValidateAcceptsOneCharacterConventionalCommitDescription(t *testing.T) {
	if err := validate("fix: x", "fix/parser", fixture(t, "completed.html"), identity{}); err != nil {
		t.Fatalf("one-character description rejected: %v", err)
	}
}

func TestValidateOnlyExemptsTrustedReleasePlease(t *testing.T) {
	id := identity{"ben-ranford/stave", "ben-ranford/stave", "release-bot", "release-bot"}
	if err := validate("chore: release 1.2.3", "release-please--branches--main", "", id); err != nil {
		t.Fatalf("trusted release PR rejected: %v", err)
	}
	if err := validate("chore(main): release 1.0.1-rc.1", "release-please--branches--main", "", id); err != nil {
		t.Fatalf("trusted release candidate rejected: %v", err)
	}
	if err := validate("chore: release 1.2.3", "release-please--branches--main", "", identity{}); err == nil {
		t.Fatal("untrusted release PR accepted")
	}
}

func TestValidatePolicy(t *testing.T) {
	if err := validatePolicy(policy{"false", "false", "true", "PR_TITLE"}); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
	if err := validatePolicy(policy{}); err == nil {
		t.Fatal("invalid policy accepted")
	}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "rendered", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}
