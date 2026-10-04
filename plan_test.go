package colfer

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// breakPackages parses the multi-package test schema with
// cross-package references.
func breakPackages(t *testing.T) Packages {
	t.Helper()
	packages, err := ParseFiles("testdata/break.colf", "testdata/break-refs.colf")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range packages {
		p.SizeMax = "16 * 1024"
		p.ListMax = "64"
	}
	return packages
}

// newTestPackage returns a package with a single struct and field.
func newTestPackage(name, structName string) *Package {
	p := &Package{Name: name, SizeMax: "1024", ListMax: "64"}
	s := &Struct{Pkg: p, Name: structName}
	f := &Field{Struct: s, Name: "v", Type: "text"}
	s.Fields = []*Field{f}
	p.Structs = []*Struct{s}
	return p
}

func plansEqual(t *testing.T, a, b *Plan) bool {
	t.Helper()
	if len(a.Files) != len(b.Files) {
		return false
	}
	for i := range a.Files {
		if a.Files[i].Path != b.Files[i].Path {
			t.Errorf("file %d: path %q != %q", i, a.Files[i].Path, b.Files[i].Path)
			return false
		}
		if !bytes.Equal(a.Files[i].Content, b.Files[i].Content) {
			t.Errorf("file %q: content mismatch", a.Files[i].Path)
			return false
		}
	}
	return true
}

func TestPlanGoDeterministic(t *testing.T) {
	dir := t.TempDir()

	plan1, err := PlanGo(dir, breakPackages(t))
	if err != nil {
		t.Fatal(err)
	}
	// A fresh parse must produce the same plan.
	plan2, err := PlanGo(dir, breakPackages(t))
	if err != nil {
		t.Fatal(err)
	}
	// A repeated call on the same schema must produce the same plan.
	packages := breakPackages(t)
	if _, err := PlanGo(dir, packages); err != nil {
		t.Fatal(err)
	}
	plan3, err := PlanGo(dir, packages)
	if err != nil {
		t.Fatal(err)
	}

	if !plansEqual(t, plan1, plan2) {
		t.Error("plan differs after a fresh parse")
	}
	if !plansEqual(t, plan1, plan3) {
		t.Error("plan differs after a repeated call on the same schema")
	}
}

func TestPlanGoNoDiskWrite(t *testing.T) {
	dir := t.TempDir()
	if _, err := PlanGo(dir, breakPackages(t)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("plan wrote to disk: found %v", entries)
	}
}

func TestPlanGoCaseFoldPackages(t *testing.T) {
	dir := t.TempDir()
	packages := Packages{newTestPackage("foo", "a"), newTestPackage("Foo", "b")}

	_, err := PlanGo(dir, packages)
	if err == nil || !strings.Contains(err.Error(), "case-insensitive") {
		t.Fatalf("got %v, want a case-fold conflict error", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("rejected plan wrote to disk: found %v", entries)
	}
}

func TestPlanGoIdentConflicts(t *testing.T) {
	tests := []struct {
		name string
		make func() *Package
		want string
	}{
		{"struct collision", func() *Package {
			p := &Package{Name: "gen", SizeMax: "1024", ListMax: "64"}
			a := &Struct{Pkg: p, Name: "foo_bar"}
			a.Fields = []*Field{{Struct: a, Name: "v", Type: "text"}}
			b := &Struct{Pkg: p, Name: "fooBar"}
			b.Fields = []*Field{{Struct: b, Name: "v", Type: "text"}}
			p.Structs = []*Struct{a, b}
			return p
		}, "both map to Go identifier FooBar"},
		{"field collision", func() *Package {
			p := &Package{Name: "gen", SizeMax: "1024", ListMax: "64"}
			s := &Struct{Pkg: p, Name: "o"}
			s.Fields = []*Field{
				{Struct: s, Name: "foo_bar", Type: "text"},
				{Struct: s, Name: "fooBar", Type: "text"},
			}
			p.Structs = []*Struct{s}
			return p
		}, "both map to Go identifier FooBar"},
		{"reserved struct", func() *Package {
			return newTestPackage("gen", "colfer_max")
		}, "reserved by the generated code"},
		{"reserved field", func() *Package {
			p := &Package{Name: "gen", SizeMax: "1024", ListMax: "64"}
			s := &Struct{Pkg: p, Name: "o"}
			s.Fields = []*Field{{Struct: s, Name: "marshal_to", Type: "text"}}
			p.Structs = []*Struct{s}
			return p
		}, "reserved by the generated code"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			_, err := PlanGo(dir, Packages{test.make()})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want an error containing %q", err, test.want)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("rejected plan wrote to disk: found %v", entries)
			}
		})
	}
}

func TestPlanGoImportAlias(t *testing.T) {
	x1 := newTestPackage("a/x", "s")
	x2 := newTestPackage("b/x", "s")
	app := &Package{Name: "app", SizeMax: "1024", ListMax: "64"}
	s := &Struct{Pkg: app, Name: "t"}
	s.Fields = []*Field{
		{Struct: s, Name: "one", Type: "s", TypeRef: x1.Structs[0]},
		{Struct: s, Name: "two", Type: "s", TypeRef: x2.Structs[0]},
	}
	app.Structs = []*Struct{s}

	plan, err := PlanGo(t.TempDir(), Packages{app, x1, x2})
	if err != nil {
		t.Fatal(err)
	}

	var content string
	for _, f := range plan.Files {
		if f.Path == "app/Colfer.go" {
			content = string(f.Content)
		}
	}
	if content == "" {
		t.Fatal("plan has no app/Colfer.go")
	}
	for _, want := range []string{`"a/x"`, `x2 "b/x"`, "*x.S", "*x2.S"} {
		if !strings.Contains(content, want) {
			t.Errorf("generated code misses %q", want)
		}
	}
}

func TestPlanCommit(t *testing.T) {
	dir := t.TempDir()
	packages := breakPackages(t)

	plan, err := PlanGo(dir, packages)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Commit(dir); err != nil {
		t.Fatal(err)
	}

	for _, f := range plan.Files {
		content, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Errorf("read %q: %s", f.Path, err)
			continue
		}
		if !bytes.Equal(content, f.Content) {
			t.Errorf("%q: committed content differs from plan", f.Path)
		}
	}
	assertNoStaging(t, dir)

	// A second plan sees the committed files as unmodified replacements.
	plan2, err := PlanGo(dir, packages)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range plan2.Files {
		if !f.replace {
			t.Errorf("%q: not marked as replacement after commit", f.Path)
		}
	}
	if err := plan2.Commit(dir); err != nil {
		t.Errorf("recommit of identical content: %s", err)
	}
	assertNoStaging(t, dir)
}

func TestPlanCommitExternalChange(t *testing.T) {
	dir := t.TempDir()
	packages := breakPackages(t)

	plan, err := PlanGo(dir, packages)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Commit(dir); err != nil {
		t.Fatal(err)
	}

	plan2, err := PlanGo(dir, packages)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, filepath.FromSlash(plan2.Files[0].Path))
	if err := os.WriteFile(target, []byte("package hacked\n"), 0o666); err != nil {
		t.Fatal(err)
	}

	err = plan2.Commit(dir)
	if err == nil || !strings.Contains(err.Error(), "changed since plan") {
		t.Fatalf("got %v, want a changed-since-plan error", err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "package hacked\n" {
		t.Errorf("externally modified file was overwritten: %q", content)
	}
	assertNoStaging(t, dir)
}

func TestPlanCommitAppeared(t *testing.T) {
	dir := t.TempDir()
	plan, err := PlanGo(dir, breakPackages(t))
	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(dir, filepath.FromSlash(plan.Files[0].Path))
	if err := os.MkdirAll(filepath.Dir(target), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package other\n"), 0o666); err != nil {
		t.Fatal(err)
	}

	err = plan.Commit(dir)
	if err == nil || !strings.Contains(err.Error(), "appeared since plan") {
		t.Fatalf("got %v, want an appeared-since-plan error", err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "package other\n" {
		t.Errorf("externally created file was overwritten: %q", content)
	}
	assertNoStaging(t, dir)
}

// rollbackPlan returns a plan which replaces keep/Colfer.go and adds
// add/Colfer.go under dir.
func rollbackPlan(t *testing.T, dir string) *Plan {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "keep"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep", "Colfer.go"), []byte("original"), 0o666); err != nil {
		t.Fatal(err)
	}

	plan := new(Plan)
	old, err := plan.newFile("keep/Colfer.go", []byte("package keep\n"))
	if err != nil {
		t.Fatal(err)
	}
	old.replace = true
	old.previous = sha256.Sum256([]byte("original"))
	if _, err := plan.newFile("add/Colfer.go", []byte("package add\n")); err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestPlanCommitRollback(t *testing.T) {
	dir := t.TempDir()
	plan := rollbackPlan(t, dir)

	// Fail the last move into place: the backup rename and the first
	// move succeed.
	renames := 0
	realRename := osRename
	osRename = func(oldname, newname string) error {
		renames++
		if renames == 3 {
			return errors.New("colfer: injected rename failure")
		}
		return realRename(oldname, newname)
	}
	defer func() { osRename = realRename }()

	err := plan.Commit(dir)
	if err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("got %v, want the injected failure", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "keep", "Colfer.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Errorf("original file not restored: %q", content)
	}
	if _, err := os.Stat(filepath.Join(dir, "add", "Colfer.go")); !os.IsNotExist(err) {
		t.Errorf("staged file left behind: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "add")); !os.IsNotExist(err) {
		t.Errorf("created directory left behind: %v", err)
	}
	assertNoStaging(t, dir)
}

func TestPlanCommitStagingFailure(t *testing.T) {
	dir := t.TempDir()
	plan := rollbackPlan(t, dir)

	realWriteFile := osWriteFile
	osWriteFile = func(string, []byte, os.FileMode) error {
		return errors.New("colfer: injected write failure")
	}
	defer func() { osWriteFile = realWriteFile }()

	err := plan.Commit(dir)
	if err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("got %v, want the injected failure", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "keep", "Colfer.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Errorf("original file not preserved: %q", content)
	}
	if _, err := os.Stat(filepath.Join(dir, "add")); !os.IsNotExist(err) {
		t.Errorf("staged output left behind: %v", err)
	}
	assertNoStaging(t, dir)
}

func TestPlanCommitValidation(t *testing.T) {
	dir := t.TempDir()

	if err := new(Plan).Commit(dir); err == nil {
		t.Error("empty plan: got nil, want an error")
	}

	escape := &Plan{Files: []*File{{Path: "../evil.go", Content: []byte("x")}}}
	if err := escape.Commit(dir); err == nil || !strings.Contains(err.Error(), "invalid path") {
		t.Errorf("escaping path: got %v, want an invalid path error", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "evil.go")); !os.IsNotExist(err) {
		t.Error("commit wrote outside the target directory")
	}

	tampered := &Plan{Files: []*File{{Path: "ok.go", Content: []byte("x")}}}
	if err := tampered.Commit(dir); err == nil || !strings.Contains(err.Error(), "tampered") {
		t.Errorf("tampered content: got %v, want a tampered content error", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ok.go")); !os.IsNotExist(err) {
		t.Error("commit wrote a file with tampered content")
	}
}

func assertNoStaging(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".colfer-staging-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("staging directories left behind: %v", matches)
	}
}
