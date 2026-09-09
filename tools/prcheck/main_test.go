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

func TestValidateKeepsHeadingsHiddenUntilTheMatchingFenceCloses(t *testing.T) {
	for _, body := range []string{
		"## Summary\n\nCompleted.\n\n````markdown\n~~~\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n\n````\n",
		"## Summary\n\nCompleted.\n\n````markdown\n```\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n\n````\n",
	} {
		if err := validate("fix: parser", "fix/parser", body, identity{}); err == nil {
			t.Fatal("headings after a mismatched or short fence close were accepted")
		}
	}
}

func TestValidateDoesNotTruncateOnLiteralUnmatchedCommentOpenersInCode(t *testing.T) {
	for _, body := range []string{
		"## Summary\n\nUse `<!--` literally.\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n",
		"## Summary\n\nCompleted.\n\n```markdown\nliteral <!--\n## hidden heading\n```\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n",
	} {
		if err := validate("fix: parser", "fix/parser", body, identity{}); err != nil {
			t.Fatalf("literal unmatched comment opener hid later sections: %v", err)
		}
	}
}

func TestValidateRejectsAnUnclosedHTMLCommentThatHidesRequiredSections(t *testing.T) {
	body := "## Summary\n\nCompleted.\n\n<!--\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n"
	if err := validate("fix: parser", "fix/parser", body, identity{}); err == nil {
		t.Fatal("headings hidden by an unclosed HTML comment were accepted")
	}
}

func TestValidateKeepsCommentSyntaxInsideInlineCodeLiteral(t *testing.T) {
	body := "## Summary\n\nUse ``<!--` and -->`` literally.\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n"
	if err := validate("fix: parser", "fix/parser", body, identity{}); err != nil {
		t.Fatalf("inline code comment syntax hid later sections: %v", err)
	}
}

func TestValidateCountsInlineCodeCommentSyntaxAsContent(t *testing.T) {
	body := "## Summary\n\n`<!-- completed -->`\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n"
	if err := validate("fix: parser", "fix/parser", body, identity{}); err != nil {
		t.Fatalf("inline code comment syntax was stripped from section content: %v", err)
	}
}

func TestValidateTreatsUnmatchedAndEscapedBackticksAsLiteral(t *testing.T) {
	for _, summary := range []string{
		"Explain the ` character.",
		"Explain the \\` character.",
	} {
		body := "## Summary\n\n" + summary + "\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n"
		if err := validate("fix: parser", "fix/parser", body, identity{}); err != nil {
			t.Fatalf("literal backtick hid later sections: %v", err)
		}
	}
}

func TestValidateDoesNotParseFencesInsideHTMLComments(t *testing.T) {
	body := "<!--\n```markdown\n-->\n## Summary\n\nCompleted.\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n"
	if err := validate("fix: parser", "fix/parser", body, identity{}); err != nil {
		t.Fatalf("fence inside HTML comment hid later sections: %v", err)
	}
}

func TestValidateRejectsHeadingsInIndentedCode(t *testing.T) {
	body := "## Summary\n\nCompleted.\n\n    ## Validation\n\n    Completed.\n\n\t## Release Notes\n\n\tCompleted.\n"
	if err := validate("fix: parser", "fix/parser", body, identity{}); err == nil {
		t.Fatal("headings in indented code were accepted")
	}
}

func TestValidateRejectsHeadingsInsideRawHTMLBlocks(t *testing.T) {
	for _, body := range []string{
		"<pre>\n## Summary\n\nCompleted.\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n</pre>\n",
		"<div>\n## Summary\n\nCompleted.\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n</div>\n",
		"<custom-widget>\n## Summary\n\nCompleted.\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n</custom-widget>\n",
	} {
		if err := validate("fix: parser", "fix/parser", body, identity{}); err == nil {
			t.Fatal("headings inside a raw HTML block were accepted")
		}
	}
}

func TestValidateRecognizesRawTextTagOpenersAtEndOfLine(t *testing.T) {
	for _, tag := range []string{"pre", "script", "style", "textarea"} {
		body := "<" + tag + "\n## Summary\n\nCompleted.\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n</" + tag + ">\n"
		if err := validate("fix: parser", "fix/parser", body, identity{}); err == nil {
			t.Fatalf("headings inside an end-of-line <%s raw block were accepted", tag)
		}
	}
}

func TestValidateRecognizesKnownHTMLBlockTagsAtEndOfLine(t *testing.T) {
	for _, tag := range []string{"<div", "</div"} {
		body := tag + "\n## Summary\n\nCompleted.\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n"
		if err := validate("fix: parser", "fix/parser", body, identity{}); err == nil {
			t.Fatalf("headings inside an end-of-line %s block were accepted", tag)
		}
	}
}

func TestValidateDoesNotTreatKnownTagPrefixesAsHTMLBlocks(t *testing.T) {
	body := "<div:foo>\n## Summary\n\nCompleted.\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n"
	if err := validate("fix: parser", "fix/parser", body, identity{}); err != nil {
		t.Fatalf("non-boundary HTML tag prefix hid required headings: %v", err)
	}
}

func TestValidateResumesAfterBlankTerminatedHTMLBlock(t *testing.T) {
	body := "<div>\n## hidden\n</div>\n\n## Summary\n\nCompleted.\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n"
	if err := validate("fix: parser", "fix/parser", body, identity{}); err != nil {
		t.Fatalf("headings after a blank-terminated HTML block were hidden: %v", err)
	}
}

func TestValidateRejectsHeadingsInsideOtherRawHTMLBlocks(t *testing.T) {
	for _, body := range []string{
		"<?processing\n## Summary\n\nCompleted.\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n?>\n",
		"<!DOCTYPE html\n## Summary\n\nCompleted.\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n>\n",
		"<![CDATA[\n## Summary\n\nCompleted.\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n]]>\n",
	} {
		if err := validate("fix: parser", "fix/parser", body, identity{}); err == nil {
			t.Fatal("headings inside a raw HTML block category were accepted")
		}
	}
}

func TestValidateDoesNotTreatAnAutolinkAsARawHTMLBlock(t *testing.T) {
	body := "<https://example.com>\n## Summary\n\nCompleted.\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n"
	if err := validate("fix: parser", "fix/parser", body, identity{}); err != nil {
		t.Fatalf("autolink hid required headings: %v", err)
	}
}

func TestValidateTreatsStandaloneRawTextClosingTagsAsBlankTerminatedHTML(t *testing.T) {
	for _, closingTag := range []string{"</pre>", "</script>"} {
		body := closingTag + "\n## Summary\n\nCompleted.\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n"
		if err := validate("fix: parser", "fix/parser", body, identity{}); err == nil {
			t.Fatal("headings after a standalone raw-text closing tag were accepted")
		}
	}
}

func TestValidateAcceptsATXClosingHashes(t *testing.T) {
	body := "## Summary ##\n\nCompleted.\n\n## Validation ##\n\nCompleted.\n\n## Release Notes ##\n\nCompleted.\n"
	if err := validate("fix: parser", "fix/parser", body, identity{}); err != nil {
		t.Fatalf("ATX closing hashes prevented required headings from matching: %v", err)
	}
}

func TestValidateRejectsEmptyMarkdownContainers(t *testing.T) {
	for _, container := range []string{">", ">>>", "*", "+", "1.", "---", "***", "___"} {
		body := "## Summary\n\n" + container + "\n\n## Validation\n\nCompleted.\n\n## Release Notes\n\nCompleted.\n"
		if err := validate("fix: parser", "fix/parser", body, identity{}); err == nil {
			t.Fatalf("empty Markdown container %q was accepted as content", container)
		}
	}
}

func TestValidateRetainsMeaningfulTextAndCodeLiterals(t *testing.T) {
	body := "## Summary\n\nN/A\n\n## Validation\n\n`>`\n\n## Release Notes\n\nCompleted.\n"
	if err := validate("fix: parser", "fix/parser", body, identity{}); err != nil {
		t.Fatalf("meaningful text or a code literal was rejected: %v", err)
	}
}

func TestValidateRejectsHeadingsAfterSpacesAndATab(t *testing.T) {
	body := "## Summary\n\nCompleted.\n\n \t## Validation\n\n \tCompleted.\n\n   \t## Release Notes\n\n   \tCompleted.\n"
	if err := validate("fix: parser", "fix/parser", body, identity{}); err == nil {
		t.Fatal("headings after spaces and a tab were accepted")
	}
}

func TestValidateAcceptsOneCharacterConventionalCommitDescription(t *testing.T) {
	if err := validate("fix: x", "fix/parser", validBody(), identity{}); err != nil {
		t.Fatalf("one-character description rejected: %v", err)
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
