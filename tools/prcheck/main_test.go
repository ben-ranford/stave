package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunAcceptsCompletedTemplate(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(nil, env(map[string]string{"PR_TITLE": "ci: add metadata check", "PR_BODY": validBody()}), &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
}

func TestValidateRejectsIncompleteMetadata(t *testing.T) {
	err := validate("bug: wrong type", "bug/42", validBody(), identity{})
	if err == nil || !strings.Contains(err.Error(), "Conventional Commit") {
		t.Fatalf("invalid title error = %v", err)
	}
	err = validate("ci: add metadata check", "ci/metadata", strings.Replace(validBody(), "A complete summary.", "-", 1), identity{})
	if err == nil || !strings.Contains(err.Error(), `section "Summary"`) {
		t.Fatalf("incomplete template error = %v", err)
	}
}

func TestValidateIgnoresTemplateContentInCodeFencesAndComments(t *testing.T) {
	body := "## Summary\n\n<!-- A complete summary. -->\n\n## Validation\n\n```markdown\n- make fast\n```\n\n## Release Notes\n\n<!-- - changelog: N/A -->\n"
	err := validate("ci: add metadata check", "ci/metadata", body, identity{})
	if err == nil || !strings.Contains(err.Error(), `section "Summary"`) {
		t.Fatalf("hidden metadata was accepted: %v", err)
	}
}

func TestValidateRejectsEmptyFencesAndCommentHeadings(t *testing.T) {
	emptyFences := "## Summary\n```\n```\n## Validation\n~~~\n~~~\n## Release Notes\n```\n```\n"
	if err := validate("ci: add metadata check", "ci/metadata", emptyFences, identity{}); err == nil {
		t.Fatal("empty fences were accepted as completed metadata")
	}
	commentHeadings := "<!--\n## Summary\ncompleted\n## Validation\ncompleted\n## Release Notes\ncompleted\n-->"
	if err := validate("ci: add metadata check", "ci/metadata", commentHeadings, identity{}); err == nil {
		t.Fatal("headings inside an HTML comment were accepted")
	}
}

func TestValidateOnlyExemptsTrustedReleasePlease(t *testing.T) {
	id := identity{"ben-ranford/stave", "ben-ranford/stave", "release-bot", "release-bot"}
	if err := validate("chore: release 1.2.3", "release-please--branches--main", "", id); err != nil {
		t.Fatalf("trusted release PR rejected: %v", err)
	}
	if err := validate("chore(main): release 1.2.3", "release-please--branches--main", "", id); err != nil {
		t.Fatalf("trusted scoped release PR rejected: %v", err)
	}
	if err := validate("chore: release 1.2.3", "release-please--branches--main", "", identity{}); err == nil {
		t.Fatal("untrusted release PR accepted")
	}
}

func TestValidateTrustedReleaseCandidate(t *testing.T) {
	id := identity{"ben-ranford/stave", "ben-ranford/stave", "release-bot", "release-bot"}
	if err := validate("chore(main): release 1.0.1-rc.1", "release-please--branches--main", "", id); err != nil {
		t.Fatalf("trusted release candidate rejected: %v", err)
	}
	id.author = "untrusted"
	if err := validate("chore(main): release 1.0.1-rc.1", "release-please--branches--main", "", id); err == nil {
		t.Fatal("untrusted release candidate accepted")
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

func validBody() string {
	return "## Summary\n\nA complete summary.\n\n## Validation\n\n- make fast\n- N/A: documentation-only change.\n\n## Release Notes\n\n- changelog: N/A\n- follow-up: None\n"
}
func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}
