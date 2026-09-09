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
	titlePattern             = regexp.MustCompile(`^(feat|fix|perf|docs|refactor|revert|test|ci|build|chore)(\([a-z0-9][a-z0-9._/-]*\))?!?: [^\s].*$`)
	orderedListMarkerPattern = regexp.MustCompile(`^[0-9]+\.$`)
	releaseTitlePattern      = regexp.MustCompile(`^chore(?:\([a-z0-9._/-]+\))?: release [0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	releaseHeadRefPattern    = regexp.MustCompile(`^release-please--branches--main(?:--components--[a-z0-9._-]+)?$`)
	requiredHeadings         = []string{"Summary", "Validation", "Release Notes"}
	placeholders             = []string{"problem:", "change:", "compatibility:", "changelog:", "follow-up:", "make fast", "make verify", "make ci"}
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
	var currentFence fence
	var currentHTML htmlBlock
	inComment := false
	inlineTicks := 0
	lines := strings.Split(body, "\n")
	for index, line := range lines {
		if currentFence.char != 0 {
			if closesFence(line, currentFence) {
				currentFence = fence{}
			}
			continue
		}
		if currentHTML.untilBlank {
			if strings.TrimSpace(line) == "" {
				currentHTML = htmlBlock{}
			}
			continue
		}
		if currentHTML.closeTag != "" {
			if closesHTMLTag(line, currentHTML.closeTag) {
				currentHTML = htmlBlock{}
			}
			continue
		}
		if currentHTML.terminator != "" {
			if strings.Contains(line, currentHTML.terminator) {
				currentHTML = htmlBlock{}
			}
			continue
		}
		canParseBlocks := !inComment && inlineTicks == 0
		if canParseBlocks {
			if opening, ok := opensHTMLBlock(line); ok {
				currentHTML = opening
				if currentHTML.closeTag != "" && closesHTMLTag(line, currentHTML.closeTag) {
					currentHTML = htmlBlock{}
				}
				if currentHTML.terminator != "" && strings.Contains(line, currentHTML.terminator) {
					currentHTML = htmlBlock{}
				}
				continue
			}
			if opening, ok := opensFence(line); ok {
				currentFence = opening
				continue
			}
		}
		if isIndentedCode(line) && canParseBlocks {
			continue
		}
		line, inComment, inlineTicks = stripHTMLCommentsLine(line, lines[index+1:], inComment, inlineTicks)
		if canParseBlocks {
			if heading, ok := sectionHeading(line); ok {
				if current != "" {
					sections[current] = strings.TrimSpace(content.String())
					content.Reset()
				}
				current = heading
				continue
			}
		}
		if current != "" {
			content.WriteString(line)
			content.WriteByte('\n')
		}
	}
	if current != "" {
		sections[current] = strings.TrimSpace(content.String())
	}
	return sections
}

type fence struct {
	char   byte
	length int
}

type htmlBlock struct {
	closeTag   string
	terminator string
	untilBlank bool
}

var genericHTMLTagPattern = regexp.MustCompile(`^</?[A-Za-z][A-Za-z0-9-]*(?:[ \t]+[^<>]*)?/?>[ \t]*$`)

var htmlBlockTags = map[string]bool{
	"address": true, "article": true, "aside": true, "base": true, "basefont": true,
	"blockquote": true, "body": true, "caption": true, "center": true, "col": true,
	"colgroup": true, "dd": true, "details": true, "dialog": true, "dir": true,
	"div": true, "dl": true, "dt": true, "fieldset": true, "figcaption": true,
	"figure": true, "footer": true, "form": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "head": true, "header": true,
	"hr": true, "html": true, "iframe": true, "legend": true, "li": true, "link": true,
	"main": true, "menu": true, "menuitem": true, "nav": true, "ol": true, "p": true,
	"picture": true, "plaintext": true, "section": true, "summary": true, "table": true,
	"tbody": true, "td": true, "tfoot": true, "th": true, "thead": true, "title": true,
	"tr": true, "track": true, "ul": true,
}

var htmlRawTextTags = map[string]bool{"pre": true, "script": true, "style": true, "textarea": true}

func opensHTMLBlock(line string) (htmlBlock, bool) {
	line, indented := trimUpToThreeSpaces(line)
	if indented || len(line) < 3 || line[0] != '<' || strings.HasPrefix(line, "<!--") {
		return htmlBlock{}, false
	}
	if strings.HasPrefix(line, "<?") {
		return htmlBlock{terminator: "?>"}, true
	}
	if strings.HasPrefix(line, "<![CDATA[") {
		return htmlBlock{terminator: "]]>"}, true
	}
	if strings.HasPrefix(line, "<!") && line[2] >= 'A' && line[2] <= 'Z' {
		return htmlBlock{terminator: ">"}, true
	}
	index := 1
	closingTag := false
	if index < len(line) && line[index] == '/' {
		closingTag = true
		index++
	}
	start := index
	for index < len(line) && (line[index] >= 'A' && line[index] <= 'Z' || line[index] >= 'a' && line[index] <= 'z' || line[index] >= '0' && line[index] <= '9' || line[index] == '-') {
		index++
	}
	if index == start {
		return htmlBlock{}, false
	}
	tag := strings.ToLower(line[start:index])
	if !closingTag && htmlRawTextTags[tag] && isRawTextTagBoundary(line, index) {
		return htmlBlock{closeTag: tag}, true
	}
	if htmlBlockTags[tag] && isHTMLBlockTagBoundary(line, index) {
		return htmlBlock{untilBlank: true}, true
	}
	if index == len(line) {
		return htmlBlock{}, false
	}
	if genericHTMLTagPattern.MatchString(line) {
		return htmlBlock{untilBlank: true}, true
	}
	return htmlBlock{}, false
}

func isRawTextTagBoundary(line string, index int) bool {
	return index == len(line) || line[index] == '>' || line[index] == ' ' || line[index] == '\t'
}

func isHTMLBlockTagBoundary(line string, index int) bool {
	return index == len(line) || line[index] == '>' || line[index] == ' ' || line[index] == '\t' || strings.HasPrefix(line[index:], "/>")
}

func closesHTMLTag(line, tag string) bool {
	lower := strings.ToLower(line)
	needle := "</" + tag
	for offset := 0; offset < len(lower); {
		found := strings.Index(lower[offset:], needle)
		if found == -1 {
			return false
		}
		start := offset + found
		end := start + len(needle)
		if end < len(lower) && (lower[end] == '>' || lower[end] == ' ' || lower[end] == '\t') {
			return true
		}
		offset = end
	}
	return false
}

func opensFence(line string) (fence, bool) {
	line, indented := trimUpToThreeSpaces(line)
	if indented || len(line) < 3 || (line[0] != '`' && line[0] != '~') {
		return fence{}, false
	}
	length := 0
	for length < len(line) && line[length] == line[0] {
		length++
	}
	if length < 3 {
		return fence{}, false
	}
	if line[0] == '`' && strings.ContainsRune(line[length:], '`') {
		return fence{}, false
	}
	return fence{char: line[0], length: length}, true
}

func closesFence(line string, opening fence) bool {
	line, indented := trimUpToThreeSpaces(line)
	if indented || len(line) < opening.length || line[0] != opening.char {
		return false
	}
	length := 0
	for length < len(line) && line[length] == opening.char {
		length++
	}
	return length >= opening.length && strings.TrimSpace(line[length:]) == ""
}

func sectionHeading(line string) (string, bool) {
	line, indented := trimUpToThreeSpaces(line)
	if indented || len(line) < 4 || !strings.HasPrefix(line, "##") || (line[2] != ' ' && line[2] != '\t') {
		return "", false
	}
	heading := strings.TrimSpace(line[3:])
	end := len(heading)
	for end > 0 && heading[end-1] == '#' {
		end--
	}
	if end < len(heading) && end > 0 && (heading[end-1] == ' ' || heading[end-1] == '\t') {
		heading = strings.TrimSpace(heading[:end])
	}
	return heading, true
}

func isIndentedCode(line string) bool {
	_, indented := trimUpToThreeSpaces(line)
	return indented
}

func trimUpToThreeSpaces(line string) (string, bool) {
	spaces := 0
	for spaces < len(line) && spaces < 4 && line[spaces] == ' ' {
		spaces++
	}
	if spaces == 4 {
		return line[spaces:], true
	}
	if spaces < len(line) && line[spaces] == '\t' {
		return line[spaces+1:], true
	}
	return line[spaces:], false
}

func stripHTMLCommentsLine(line string, remaining []string, inComment bool, inlineTicks int) (string, bool, int) {
	var visible strings.Builder
	for index := 0; index < len(line); {
		if inComment {
			end := strings.Index(line[index:], "-->")
			if end == -1 {
				return visible.String(), true, inlineTicks
			}
			index += end + 3
			inComment = false
			continue
		}
		if line[index] == '`' && !isEscapedBacktick(line, index) {
			length := 1
			for index+length < len(line) && line[index+length] == '`' {
				length++
			}
			visible.WriteString(line[index : index+length])
			if inlineTicks == 0 && inlineBlockHasClosingRun(line[index+length:], remaining, length) {
				inlineTicks = length
			} else if inlineTicks == length {
				inlineTicks = 0
			}
			index += length
			continue
		}
		if inlineTicks == 0 && strings.HasPrefix(line[index:], "<!--") {
			inComment = true
			index += 4
			continue
		}
		visible.WriteByte(line[index])
		index++
	}
	return visible.String(), inComment, inlineTicks
}

func isEscapedBacktick(line string, index int) bool {
	backslashes := 0
	for index > backslashes && line[index-backslashes-1] == '\\' {
		backslashes++
	}
	return backslashes%2 == 1
}

func inlineBlockHasClosingRun(line string, remaining []string, length int) bool {
	if containsUnescapedBacktickRun(line, length) {
		return true
	}
	for _, line := range remaining {
		if strings.TrimSpace(line) == "" {
			return false
		}
		if _, heading := sectionHeading(line); heading {
			return false
		}
		if containsUnescapedBacktickRun(line, length) {
			return true
		}
	}
	return false
}

func containsUnescapedBacktickRun(line string, length int) bool {
	for index := 0; index < len(line); {
		if line[index] != '`' || isEscapedBacktick(line, index) {
			index++
			continue
		}
		run := 1
		for index+run < len(line) && line[index+run] == '`' {
			run++
		}
		if run == length {
			return true
		}
		index += run
	}
	return false
}

func stripHTMLComments(body string) string {
	var visible strings.Builder
	inComment := false
	inlineTicks := 0
	lines := strings.Split(body, "\n")
	for index, line := range lines {
		line, inComment, inlineTicks = stripHTMLCommentsLine(line, lines[index+1:], inComment, inlineTicks)
		visible.WriteString(line)
		visible.WriteByte('\n')
	}
	return visible.String()
}

func meaningful(content string) bool {
	content = stripHTMLComments(content)
	for _, placeholder := range placeholders {
		content = strings.ReplaceAll(strings.ToLower(content), placeholder, "")
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !isEmptyMarkdownContainer(line) && !strings.HasPrefix(line, "- [ ]") {
			return true
		}
	}
	return false
}

func isEmptyMarkdownContainer(line string) bool {
	if line == "-" || line == "*" || line == "+" {
		return true
	}
	if orderedListMarkerPattern.MatchString(line) {
		return true
	}
	withoutWhitespace := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, line)
	if len(withoutWhitespace) > 0 && strings.Trim(withoutWhitespace, ">") == "" {
		return true
	}
	if len(withoutWhitespace) < 3 {
		return false
	}
	marker := withoutWhitespace[0]
	if marker != '-' && marker != '*' && marker != '_' {
		return false
	}
	for index := 1; index < len(withoutWhitespace); index++ {
		if withoutWhitespace[index] != marker {
			return false
		}
	}
	return true
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
