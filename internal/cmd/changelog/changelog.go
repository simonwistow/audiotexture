package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A changelog in the Keep a Changelog layout: a preamble, then one "## [name]"
// section per release, newest first, with "## [Unreleased]" on top, and a
// block of "[name]: url" link references at the end.
type changelog struct {
	lines    []string
	sections []section
	links    map[string]int // link name, as written, to its line
}

type section struct {
	name  string // "Unreleased" or a version without the "v"
	date  string // "" for Unreleased
	start int    // line of the heading
	end   int    // line after the last line of the body
}

var (
	headingRE = regexp.MustCompile(`^## \[([^\]]+)\](?: - (\S+))?\s*$`)
	linkRE    = regexp.MustCompile(`^\[([^\]]+)\]: (\S+)\s*$`)
	versionRE = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$`)
)

func parse(text string) *changelog {
	c := &changelog{lines: strings.Split(text, "\n"), links: map[string]int{}}
	for i, line := range c.lines {
		if m := headingRE.FindStringSubmatch(line); m != nil {
			if n := len(c.sections); n > 0 {
				c.sections[n-1].end = i
			}
			c.sections = append(c.sections, section{name: m[1], date: m[2], start: i})
		}
		if m := linkRE.FindStringSubmatch(line); m != nil {
			c.links[strings.ToLower(m[1])] = i
		}
	}
	if n := len(c.sections); n > 0 {
		// The last section runs up to the link references, not over them.
		end := len(c.lines)
		for i := c.sections[n-1].start + 1; i < len(c.lines); i++ {
			if linkRE.MatchString(c.lines[i]) {
				end = i
				break
			}
		}
		c.sections[n-1].end = end
	}
	return c
}

func (c *changelog) String() string { return strings.Join(c.lines, "\n") }

// body returns a section's text without its heading, trimmed of blank lines.
func (c *changelog) body(s section) string {
	return strings.Trim(strings.Join(c.lines[s.start+1:s.end], "\n"), "\n ")
}

func (c *changelog) find(name string) (section, bool) {
	for _, s := range c.sections {
		if s.name == name {
			return s, true
		}
	}
	return section{}, false
}

// lint checks the structure Keep a Changelog asks for.
func (c *changelog) lint() error {
	var errs []error
	if len(c.sections) == 0 || c.sections[0].name != "Unreleased" {
		errs = append(errs, errors.New(`the first section must be "## [Unreleased]"`))
	}
	var prev *section
	for i := range c.sections {
		s := &c.sections[i]
		if _, ok := c.links[strings.ToLower(s.name)]; !ok {
			errs = append(errs, fmt.Errorf("[%s] has no link reference at the end of the file", s.name))
		}
		if s.name == "Unreleased" {
			if i != 0 {
				errs = append(errs, errors.New("[Unreleased] must be the first section"))
			}
			continue
		}
		if !versionRE.MatchString(s.name) {
			errs = append(errs, fmt.Errorf("[%s] is not a semantic version such as 1.2.3", s.name))
			continue
		}
		if _, err := time.Parse(time.DateOnly, s.date); err != nil {
			errs = append(errs, fmt.Errorf("[%s] needs a release date, as \"## [%s] - YYYY-MM-DD\"", s.name, s.name))
		}
		if prev != nil {
			if compareVersions(prev.name, s.name) <= 0 {
				errs = append(errs, fmt.Errorf("[%s] comes before [%s] but is not newer", prev.name, s.name))
			}
			if prev.date != "" && s.date != "" && prev.date < s.date {
				errs = append(errs, fmt.Errorf("[%s] is dated before [%s], which it comes above", prev.name, s.name))
			}
		}
		prev = s
	}
	return errors.Join(errs...)
}

// check confirms the changelog is ready for tag: well formed, with a dated,
// non-empty section for its version at the top.
func (c *changelog) check(tag string) error {
	v, err := tagVersion(tag)
	if err != nil {
		return err
	}
	if err := c.lint(); err != nil {
		return err
	}
	s, ok := c.find(v)
	if !ok {
		return fmt.Errorf("no \"## [%s]\" section: run `go run ./internal/cmd/changelog release %s` before tagging", v, v)
	}
	if len(c.sections) < 2 || c.sections[1].name != v {
		return fmt.Errorf("[%s] is not the newest release in the changelog", v)
	}
	if c.body(s) == "" {
		return fmt.Errorf("[%s] has no entries", v)
	}
	return nil
}

// notes returns the body of tag's section, for a release description.
func (c *changelog) notes(tag string) (string, error) {
	v, err := tagVersion(tag)
	if err != nil {
		return "", err
	}
	s, ok := c.find(v)
	if !ok {
		return "", fmt.Errorf("no \"## [%s]\" section", v)
	}
	return c.body(s), nil
}

// release turns the Unreleased section into version v, dated date, opens a
// fresh empty Unreleased above it, and points the link references at the
// new tag.
func (c *changelog) release(v, date string) error {
	if !versionRE.MatchString(v) {
		return fmt.Errorf("%q is not a semantic version such as 1.2.3", v)
	}
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return fmt.Errorf("%q is not a date as YYYY-MM-DD", date)
	}
	if err := c.lint(); err != nil {
		return err
	}
	if _, ok := c.find(v); ok {
		return fmt.Errorf("[%s] is already in the changelog", v)
	}
	unreleased := c.sections[0]
	if c.body(unreleased) == "" {
		return errors.New("[Unreleased] has no entries to release")
	}
	var previous string
	if len(c.sections) > 1 {
		previous = c.sections[1].name
		if compareVersions(v, previous) <= 0 {
			return fmt.Errorf("%s is not newer than the last release, %s", v, previous)
		}
	}

	unreleasedLink := c.links["unreleased"]
	base := repoURL(c.lines[unreleasedLink])
	if base == "" {
		return fmt.Errorf("cannot find a GitHub repository URL in %q", c.lines[unreleasedLink])
	}
	versionLink := fmt.Sprintf("[%s]: %s/releases/tag/v%s", v, base, v)
	if previous != "" {
		versionLink = fmt.Sprintf("[%s]: %s/compare/v%s...v%s", v, base, previous, v)
	}

	// Edit from the bottom up so earlier line numbers stay valid.
	lines := c.lines
	lines = replaceLine(lines, unreleasedLink,
		fmt.Sprintf("[Unreleased]: %s/compare/v%s...HEAD", base, v),
		versionLink)
	lines = replaceLine(lines, unreleased.start,
		"## [Unreleased]",
		"",
		fmt.Sprintf("## [%s] - %s", v, date))
	*c = *parse(strings.Join(lines, "\n"))
	return c.lint()
}

func replaceLine(lines []string, i int, with ...string) []string {
	out := append([]string(nil), lines[:i]...)
	out = append(out, with...)
	return append(out, lines[i+1:]...)
}

var repoURLRE = regexp.MustCompile(`https://github\.com/[^/\s]+/[^/\s]+`)

// repoURL finds the repository a link reference points into.
func repoURL(link string) string {
	m := linkRE.FindStringSubmatch(link)
	if m == nil {
		return ""
	}
	return strings.TrimSuffix(repoURLRE.FindString(m[2]), ".git")
}

// tagVersion turns a tag such as v1.2.3 into the version the changelog
// heading uses, 1.2.3.
func tagVersion(tag string) (string, error) {
	tag = strings.TrimPrefix(tag, "refs/tags/")
	v, ok := strings.CutPrefix(tag, "v")
	if !ok || !versionRE.MatchString(v) {
		return "", fmt.Errorf("tag %q is not a version tag such as v1.2.3", tag)
	}
	return v, nil
}

// compareVersions orders two semantic versions, returning -1, 0 or +1. A
// pre-release sorts before its release; pre-releases compare as strings,
// which is enough for rc1 < rc2.
func compareVersions(a, b string) int {
	ma, mb := versionRE.FindStringSubmatch(a), versionRE.FindStringSubmatch(b)
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(ma[i])
		y, _ := strconv.Atoi(mb[i])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch pa, pb := ma[4], mb[4]; {
	case pa == pb:
		return 0
	case pa == "":
		return 1
	case pb == "":
		return -1
	default:
		return strings.Compare(pa, pb)
	}
}
