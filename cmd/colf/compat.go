package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/pascaldekloe/colfer"
)

// runCompat implements the "compat" subcommand: it parses two schema
// revisions, audits wire-format compatibility in both directions and prints a
// stable report. It never invokes a code generator, so neither revision's
// NameNative/TypeNative generation state is written.
func runCompat(args []string) {
	fs := flag.NewFlagSet("compat", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: colf compat [options]")
		fs.PrintDefaults()
		fmt.Fprintln(os.Stderr, `
Audits whether two Colfer schema revisions can read each other's wire data.
The audit reasons from the actual wire format and the generated decoder:
fields are matched by declaration index (the wire header), appended fields are
not assumed universally compatible, recursive type graphs terminate and keep
the offending path, and limits/extensions that cannot be proven are reported
as unknown.`)
	}

	var (
		oldFiles = fs.String("old", "", "Comma-separated schema files/directories for the old revision.")
		newFiles = fs.String("new", "", "Comma-separated schema files/directories for the new revision.")
		oldSize  = fs.String("old-size-max", "16 * 1024 * 1024", "ColferSizeMax expression used to generate the old revision.")
		newSize  = fs.String("new-size-max", "16 * 1024 * 1024", "ColferSizeMax expression used to generate the new revision.")
		oldList  = fs.String("old-list-max", "64 * 1024", "ColferListMax expression used to generate the old revision.")
		newList  = fs.String("new-list-max", "64 * 1024", "ColferListMax expression used to generate the new revision.")
		asJSON   = fs.Bool("json", false, "Emit the report as JSON instead of text.")
		quiet    = fs.Bool("q", false, "Exit 0 even when an incompatibility was proven (still prints the report).")
	)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}

	if strings.TrimSpace(*oldFiles) == "" || strings.TrimSpace(*newFiles) == "" {
		fmt.Fprintln(os.Stderr, `colf compat: both -old and -new are required`)
		fs.Usage()
		os.Exit(2)
	}

	oldPaths, err := resolveCompatFiles(splitList(*oldFiles))
	if err != nil {
		log.Fatal(err)
	}
	newPaths, err := resolveCompatFiles(splitList(*newFiles))
	if err != nil {
		log.Fatal(err)
	}

	oldPkgs, err := colfer.ParseFiles(oldPaths...)
	if err != nil {
		log.Fatalf("colf compat: old schema: %s", err)
	}
	newPkgs, err := colfer.ParseFiles(newPaths...)
	if err != nil {
		log.Fatalf("colf compat: new schema: %s", err)
	}

	oldBounds := map[string]colfer.Limits{}
	for _, p := range oldPkgs {
		oldBounds[p.Name] = colfer.Limits{SizeMax: *oldSize, ListMax: *oldList}
	}
	newBounds := map[string]colfer.Limits{}
	for _, p := range newPkgs {
		newBounds[p.Name] = colfer.Limits{SizeMax: *newSize, ListMax: *newList}
	}

	rep := colfer.Compare(oldPkgs, newPkgs, oldBounds, newBounds)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			log.Fatal(err)
		}
	} else {
		if err := rep.WriteText(os.Stdout); err != nil {
			log.Fatal(err)
		}
	}

	if !*quiet && (rep.OldToNew == colfer.VerdictIncompatible || rep.NewToOld == colfer.VerdictIncompatible) {
		os.Exit(1)
	}
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func resolveCompatFiles(inputs []string) ([]string, error) {
	var paths []string
	for _, in := range inputs {
		info, err := os.Stat(in)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			paths = append(paths, in)
			continue
		}
		children, err := filepath.Glob(filepath.Join(in, "*.colf"))
		if err != nil {
			return nil, err
		}
		paths = append(paths, children...)
	}
	return paths, nil
}
