package main

import (
	"os"
	"strings"
	"testing"
)

const unreleasedOnly = `# Changelog

Preamble.

## [Unreleased]

### Added

- Something.

[Unreleased]: https://github.com/example/project/commits/main
`

// TestProjectChangelog keeps the real file passing lint, the same check CI
// runs.
func TestProjectChangelog(t *testing.T) {
	raw, err := os.ReadFile("../../../CHANGELOG.md")
	if err != nil {
		t.Fatalf("reading CHANGELOG.md: %v", err)
	}
	if err := parse(string(raw)).lint(); err != nil {
		t.Errorf("CHANGELOG.md: %v", err)
	}
}

func TestReleaseFirstVersion(t *testing.T) {
	c := parse(unreleasedOnly)
	if err := c.release("0.1.0", "2026-10-06"); err != nil {
		t.Fatalf("release: %v", err)
	}
	want := `# Changelog

Preamble.

## [Unreleased]

## [0.1.0] - 2026-10-06

### Added

- Something.

[Unreleased]: https://github.com/example/project/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/example/project/releases/tag/v0.1.0
`
	if got := c.String(); got != want {
		t.Errorf("after release:\n%s\nwant:\n%s", got, want)
	}
	if err := c.check("v0.1.0"); err != nil {
		t.Errorf("check after release: %v", err)
	}
	notes, err := c.notes("v0.1.0")
	if err != nil {
		t.Fatalf("notes: %v", err)
	}
	if notes != "### Added\n\n- Something." {
		t.Errorf("notes = %q", notes)
	}
}

func TestReleaseSecondVersion(t *testing.T) {
	c := parse(unreleasedOnly)
	if err := c.release("0.1.0", "2026-10-06"); err != nil {
		t.Fatalf("first release: %v", err)
	}
	// New work lands under the fresh Unreleased heading.
	c = parse(strings.Replace(c.String(), "## [Unreleased]\n", "## [Unreleased]\n\n### Fixed\n\n- A bug.\n", 1))
	if err := c.release("0.2.0", "2026-11-01"); err != nil {
		t.Fatalf("second release: %v", err)
	}
	for _, line := range []string{
		"[Unreleased]: https://github.com/example/project/compare/v0.2.0...HEAD",
		"[0.2.0]: https://github.com/example/project/compare/v0.1.0...v0.2.0",
		"[0.1.0]: https://github.com/example/project/releases/tag/v0.1.0",
	} {
		if !strings.Contains(c.String(), line+"\n") {
			t.Errorf("missing link %q in:\n%s", line, c)
		}
	}
	notes, err := c.notes("v0.2.0")
	if err != nil || notes != "### Fixed\n\n- A bug." {
		t.Errorf("notes = %q, %v", notes, err)
	}
	if err := c.check("v0.1.0"); err == nil {
		t.Error("check passed for a release that is no longer the newest")
	}
}

func TestReleaseRefuses(t *testing.T) {
	released := parse(unreleasedOnly)
	if err := released.release("0.1.0", "2026-10-06"); err != nil {
		t.Fatalf("release: %v", err)
	}
	for _, tc := range []struct {
		name, text, version, date string
	}{
		{"empty unreleased", released.String(), "0.2.0", "2026-10-07"},
		{"not a version", unreleasedOnly, "one", "2026-10-06"},
		{"not a date", unreleasedOnly, "0.1.0", "6/10/2026"},
		{"older than the last", strings.Replace(released.String(), "## [Unreleased]\n", "## [Unreleased]\n\n- New.\n", 1), "0.0.9", "2026-10-07"},
		{"already released", strings.Replace(released.String(), "## [Unreleased]\n", "## [Unreleased]\n\n- New.\n", 1), "0.1.0", "2026-10-07"},
	} {
		if err := parse(tc.text).release(tc.version, tc.date); err == nil {
			t.Errorf("%s: release succeeded, want an error", tc.name)
		}
	}
}

func TestCheckRefuses(t *testing.T) {
	for _, tc := range []struct{ name, tag string }{
		{"not released yet", "v0.1.0"},
		{"not a version tag", "0.1.0"},
		{"not a version", "vnext"},
	} {
		if err := parse(unreleasedOnly).check(tc.tag); err == nil {
			t.Errorf("%s: check(%q) passed, want an error", tc.name, tc.tag)
		}
	}
}

func TestLint(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"no unreleased", "## [0.1.0] - 2026-10-06\n\n- A.\n\n[0.1.0]: https://github.com/e/p/releases/tag/v0.1.0\n"},
		{"no date", "## [Unreleased]\n\n## [0.1.0]\n\n- A.\n\n[Unreleased]: https://github.com/e/p/compare/v0.1.0...HEAD\n[0.1.0]: https://github.com/e/p/releases/tag/v0.1.0\n"},
		{"no link", "## [Unreleased]\n\n## [0.1.0] - 2026-10-06\n\n- A.\n\n[Unreleased]: https://github.com/e/p/compare/v0.1.0...HEAD\n"},
		{"out of order", "## [Unreleased]\n\n## [0.1.0] - 2026-10-06\n\n## [0.2.0] - 2026-10-07\n\n[Unreleased]: x\n[0.1.0]: x\n[0.2.0]: x\n"},
		{"not semver", "## [Unreleased]\n\n## [one] - 2026-10-06\n\n[Unreleased]: x\n[one]: x\n"},
	} {
		if err := parse(tc.text).lint(); err == nil {
			t.Errorf("%s: lint passed, want an error", tc.name)
		}
	}
	if err := parse(unreleasedOnly).lint(); err != nil {
		t.Errorf("lint of a valid changelog: %v", err)
	}
}

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.10", "1.0.9", 1},
		{"0.9.0", "0.10.0", -1},
		{"1.0.0-rc1", "1.0.0", -1},
		{"1.0.0-rc2", "1.0.0-rc1", 1},
	} {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
