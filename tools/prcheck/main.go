// prcheck validates pull-request metadata supplied by the trusted workflow.
package main

import (
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxBodyBytes = 1 << 20

var (
	titlePattern          = regexp.MustCompile(`^(feat|fix|perf|docs|refactor|revert|test|ci|build|chore)(\([a-z0-9][a-z0-9._/-]*\))?!?: [^\s].*$`)
	releaseTitlePattern   = regexp.MustCompile(`^chore(?:\([a-z0-9._/-]+\))?: release [0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	releaseHeadRefPattern = regexp.MustCompile(`^release-please--branches--main(?:--components--[a-z0-9._-]+)?$`)
	requiredHeadings      = []string{"Summary", "Validation", "Release Notes"}
	placeholders          = []string{"problem:", "change:", "compatibility:", "changelog:", "follow-up:", "make fast", "make verify", "make ci"}
)

type identity struct{ headRepo, repo, releaseAuthor, author string }
type policy struct{ merge, rebase, squash, squashTitle string }
type section struct {
	heading    string
	meaningful bool
}

func main() { os.Exit(run(os.Args[1:], os.Getenv, os.Stderr)) }

func run(args []string, getenv func(string) string, stderr io.Writer) int {
	fs := flag.NewFlagSet("prcheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	title := fs.String("title", getenv("PR_TITLE"), "pull request title")
	headRef := fs.String("head-ref", getenv("PR_HEAD_REF"), "pull request head ref")
	headRepo := fs.String("head-repo-full-name", getenv("PR_HEAD_REPO_FULL_NAME"), "pull request head repository")
	repo := fs.String("repo-full-name", getenv("REPOSITORY_FULL_NAME"), "repository")
	releaseAuthor := fs.String("release-please-author-login", getenv("RELEASE_PLEASE_AUTHOR_LOGIN"), "trusted Release Please author")
	author := fs.String("pr-author-login", getenv("PR_AUTHOR_LOGIN"), "pull request author")
	bodyFile := fs.String("body-file", "", "path to UTF-8 GitHub-rendered pull request body HTML")
	checkPolicy := fs.Bool("check-repo-policy", false, "validate merge policy")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	body, err := readBody(*bodyFile, getenv)
	if err != nil {
		return fail(stderr, "read rendered PR body HTML: %v", err)
	}
	if err := validate(*title, *headRef, body, identity{*headRepo, *repo, *releaseAuthor, *author}); err != nil {
		return fail(stderr, "%v", err)
	}
	if *checkPolicy {
		if err := validatePolicy(policy{getenv("REPO_ALLOW_MERGE_COMMIT"), getenv("REPO_ALLOW_REBASE_MERGE"), getenv("REPO_ALLOW_SQUASH_MERGE"), getenv("REPO_SQUASH_MERGE_COMMIT_TITLE")}); err != nil {
			return fail(stderr, "%v", err)
		}
	}
	return 0
}

func readBody(path string, getenv func(string) string) (string, error) {
	if path == "" {
		return getenv("PR_BODY"), nil
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("body file is not a regular file")
	}
	if info.Size() > maxBodyBytes {
		return "", fmt.Errorf("body exceeds %d byte limit", maxBodyBytes)
	}
	body, err := os.ReadFile(path)
	return string(body), err
}

func fail(stderr io.Writer, format string, args ...any) int {
	_, _ = fmt.Fprintf(stderr, format+"\n", args...)
	return 1
}

func validate(title, headRef, renderedHTML string, id identity) error {
	title = strings.TrimSpace(title)
	if !titlePattern.MatchString(title) {
		return errors.New("PR title must be a Conventional Commit title using feat, fix, perf, docs, refactor, revert, test, ci, build, or chore")
	}
	if releaseHeadRefPattern.MatchString(strings.TrimSpace(headRef)) && releaseTitlePattern.MatchString(title) && id.headRepo != "" && id.headRepo == id.repo && id.releaseAuthor != "" && id.releaseAuthor == id.author {
		return nil
	}
	sections, err := renderedSections(renderedHTML)
	if err != nil {
		return fmt.Errorf("rendered PR body HTML is invalid: %w", err)
	}
	var failures []string
	for _, heading := range requiredHeadings {
		content, ok := sections[heading]
		if !ok {
			failures = append(failures, fmt.Sprintf("PR body is missing required template section %q", heading))
			continue
		}
		if !content {
			failures = append(failures, fmt.Sprintf("PR section %q must be completed; replace placeholder text with real content or N/A", heading))
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "\n"))
	}
	return nil
}

// renderedSections validates the GitHub-rendered HTML, so Markdown syntax cannot
// masquerade as a heading or as visible section content.
func renderedSections(renderedHTML string) (map[string]bool, error) {
	decoder := xml.NewDecoder(strings.NewReader(renderedHTML))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose
	decoder.Entity = xml.HTMLEntity

	var sections []section
	var current *section
	var headingText strings.Builder
	headingDepth := 0
	hiddenDepth := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch token := token.(type) {
		case xml.StartElement:
			name := strings.ToLower(token.Name.Local)
			if name == "h2" {
				headingDepth++
				if headingDepth == 1 {
					headingText.Reset()
					sections = append(sections, section{})
					current = &sections[len(sections)-1]
				}
			}
			if hiddenTextElement(name) {
				hiddenDepth++
			}
		case xml.EndElement:
			name := strings.ToLower(token.Name.Local)
			if hiddenTextElement(name) && hiddenDepth > 0 {
				hiddenDepth--
			}
			if name == "h2" && headingDepth > 0 {
				headingDepth--
				if headingDepth == 0 && current != nil {
					current.heading = strings.Join(strings.Fields(headingText.String()), " ")
				}
			}
		case xml.CharData:
			if headingDepth > 0 {
				headingText.Write([]byte(token))
				continue
			}
			if current != nil && hiddenDepth == 0 && meaningfulText(string(token)) {
				current.meaningful = true
			}
		}
	}

	completed := make(map[string]bool)
	for _, candidate := range sections {
		for _, heading := range requiredHeadings {
			if candidate.heading == heading {
				completed[heading] = candidate.meaningful
			}
		}
	}
	return completed, nil
}

func hiddenTextElement(name string) bool {
	switch name {
	case "head", "script", "style", "template":
		return true
	default:
		return false
	}
}

func meaningfulText(text string) bool {
	text = strings.ToLower(text)
	for _, placeholder := range placeholders {
		text = strings.ReplaceAll(text, placeholder, "")
	}
	return strings.TrimSpace(text) != ""
}

func validatePolicy(p policy) error {
	checks := []struct{ actual, expected, message string }{
		{p.squash, "true", "Repository must allow squash merges"},
		{p.merge, "false", "Repository must disable merge commits"},
		{p.rebase, "false", "Repository must disable rebase merges"},
		{p.squashTitle, "PR_TITLE", "Repository squash merge titles must default to the PR title"},
	}
	var failures []string
	for _, check := range checks {
		if strings.TrimSpace(check.actual) != check.expected {
			failures = append(failures, fmt.Sprintf("%s (expected %q, got %q)", check.message, check.expected, strings.TrimSpace(check.actual)))
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "\n"))
	}
	return nil
}
