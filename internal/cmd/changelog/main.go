// Command changelog keeps CHANGELOG.md in the Keep a Changelog layout and
// ties it to releases.
//
//	changelog lint               check the file's structure
//	changelog check v1.2.3       check it is ready for tag v1.2.3
//	changelog notes v1.2.3       print the v1.2.3 section, for release notes
//	changelog release 1.2.3      turn [Unreleased] into [1.2.3], dated today
//
// It is a development tool, not part of the published surface. Run it from
// the repository root, or pass -file.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	file := flag.String("file", "CHANGELOG.md", "the changelog to read")
	date := flag.String("date", time.Now().Format(time.DateOnly), "release date, for release")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: changelog [-file CHANGELOG.md] lint | check vX.Y.Z | notes vX.Y.Z | [-date YYYY-MM-DD] release X.Y.Z")
	}
	flag.Parse()
	if err := run(*file, *date, flag.Args()); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", *file, err)
		os.Exit(1)
	}
}

func run(file, date string, args []string) error {
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	c := parse(string(raw))

	arg := func() (string, error) {
		if len(args) != 2 {
			return "", fmt.Errorf("%s takes one argument, a version", args[0])
		}
		return args[1], nil
	}

	switch args[0] {
	case "lint":
		return c.lint()
	case "check":
		tag, err := arg()
		if err != nil {
			return err
		}
		return c.check(tag)
	case "notes":
		tag, err := arg()
		if err != nil {
			return err
		}
		notes, err := c.notes(tag)
		if err != nil {
			return err
		}
		fmt.Println(notes)
		return nil
	case "release":
		v, err := arg()
		if err != nil {
			return err
		}
		if err := c.release(v, date); err != nil {
			return err
		}
		if err := os.WriteFile(file, []byte(c.String()), 0o644); err != nil {
			return err
		}
		fmt.Printf("%s: [Unreleased] is now [%s] - %s. Commit it, then tag v%s.\n", file, v, date, v)
		return nil
	default:
		flag.Usage()
		os.Exit(2)
		return nil
	}
}
