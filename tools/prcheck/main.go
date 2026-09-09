// prcheck validates pull-request metadata supplied by the trusted workflow.
package main

import (
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
	titlePattern          = regexp.MustCompile(`^(feat|fix|perf|docs|refactor|revert|test|ci|build|chore)(\([a-z0-9][a-z0-9._/-]*\))?!?: [^\s].+$`)
	releaseTitlePattern   = regexp.MustCompile(`^chore(?:\([a-z0-9._/-]+\))?: release [0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	releaseHeadRefPattern = regexp.MustCompile(`^release-please--branches--main(?:--components--[a-z0-9._-]+)?$`)
	requiredHeadings      = []string{"Summary", "Validation", "Release Notes"}
	placeholders          = []string{"problem:", "change:", "compatibility:", "changelog:", "follow-up:", "make fast", "make verify", "make ci"}
)

type identity struct{ headRepo, repo, releaseAuthor, author string }
type policy struct{ merge, rebase, squash, squashTitle string }

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
	bodyFile := fs.String("body-file", "", "path to pull request body")
	checkPolicy := fs.Bool("check-repo-policy", false, "validate merge policy")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	body, err := readBody(*bodyFile, getenv)
	if err != nil {
		return fail(stderr, "read PR body: %v", err)
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

func validate(title, headRef, body string, id identity) error {
	title = strings.TrimSpace(title)
	if !titlePattern.MatchString(title) {
		return errors.New("PR title must be a Conventional Commit title using feat, fix, perf, docs, refactor, revert, test, ci, build, or chore")
	}
	if releaseHeadRefPattern.MatchString(strings.TrimSpace(headRef)) && releaseTitlePattern.MatchString(title) && id.headRepo != "" && id.headRepo == id.repo && id.releaseAuthor != "" && id.releaseAuthor == id.author {
		return nil
	}
	sections := parseSections(body)
	var failures []string
	for _, heading := range requiredHeadings {
		content, ok := sections[heading]
		if !ok {
			failures = append(failures, fmt.Sprintf("PR body is missing required template section %q", heading))
			continue
		}
		if !meaningful(content) {
			failures = append(failures, fmt.Sprintf("PR section %q must be completed; replace placeholder text with real content or N/A", heading))
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "\n"))
	}
	return nil
}

func parseSections(body string) map[string]string {
	sections := make(map[string]string)
	var current string
	var content strings.Builder
	inFence := false
	for _, line := range strings.Split(stripHTMLComments(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if !inFence && strings.HasPrefix(trimmed, "## ") {
			if current != "" {
				sections[current] = strings.TrimSpace(content.String())
				content.Reset()
			}
			current = strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			continue
		}
		if current != "" && !inFence {
			content.WriteString(line)
			content.WriteByte('\n')
		}
	}
	if current != "" {
		sections[current] = strings.TrimSpace(content.String())
	}
	return sections
}

func stripHTMLComments(body string) string {
	for {
		start := strings.Index(body, "<!--")
		if start == -1 {
			return body
		}
		end := strings.Index(body[start+4:], "-->")
		if end == -1 {
			return body[:start]
		}
		body = body[:start] + body[start+4+end+3:]
	}
}

func meaningful(content string) bool {
	content = regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(content, "")
	for _, placeholder := range placeholders {
		content = strings.ReplaceAll(strings.ToLower(content), placeholder, "")
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && line != "-" && !strings.HasPrefix(line, "- [ ]") {
			return true
		}
	}
	return false
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
