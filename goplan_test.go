package colfer

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// planTestPackages returns two packages with a cross reference.
func planTestPackages() Packages {
	p1 := &Package{Name: "alpha", SizeMax: "1024", ListMax: "64"}
	s1 := &Struct{Name: "one", Pkg: p1}
	s1.Fields = []*Field{
		{Name: "n", Struct: s1, Type: "uint32"},
		{Name: "ts", Struct: s1, Type: "timestamp"},
	}
	p1.Structs = []*Struct{s1}

	p2 := &Package{Name: "beta", SizeMax: "1024", ListMax: "64"}
	s2 := &Struct{Name: "two", Pkg: p2}
	s2.Fields = []*Field{
		{Name: "ref", Struct: s2, Type: "one", TypeRef: s1},
		{Name: "f", Struct: s2, Type: "float64"},
	}
	p2.Structs = []*Struct{s2}

	return Packages{p1, p2}
}

func planData(t *testing.T, plan *GoPlan) map[string][]byte {
	t.Helper()
	m := make(map[string][]byte, len(plan.Files))
	for _, f := range plan.Files {
		m[f.Path] = f.Data
	}
	return m
}

func TestPlanGoDeterministic(t *testing.T) {
	dir := t.TempDir()

	first, err := PlanGo(dir, planTestPackages())
	if err != nil {
		t.Fatal("first plan:", err)
	}
	// a commit in between must not affect later plans
	if err := first.Commit(); err != nil {
		t.Fatal("commit:", err)
	}
	second, err := PlanGo(dir, planTestPackages())
	if err != nil {
		t.Fatal("second plan:", err)
	}
	// package order must not affect the plan either
	reverse := planTestPackages()
	reverse[0], reverse[1] = reverse[1], reverse[0]
	third, err := PlanGo(dir, reverse)
	if err != nil {
		t.Fatal("reversed plan:", err)
	}

	a, b, c := planData(t, first), planData(t, second), planData(t, third)
	if len(a) != 2 {
		t.Fatalf("got %d plan files, want 2", len(a))
	}
	for path, data := range a {
		if !bytes.Equal(data, b[path]) {
			t.Errorf("file %q changed after a commit", path)
		}
		if !bytes.Equal(data, c[path]) {
			t.Errorf("file %q changed with reversed package order", path)
		}
	}
	if first.Files[0].Path >= first.Files[1].Path {
		t.Errorf("plan files not sorted: %q, %q", first.Files[0].Path, first.Files[1].Path)
	}
}

func TestPlanGoNoWrite(t *testing.T) {
	dir := t.TempDir()
	if _, err := PlanGo(dir, planTestPackages()); err != nil {
		t.Fatal("plan:", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("plan wrote to disk: %v", entries)
	}
}

func TestPlanGoCaseConflict(t *testing.T) {
	dir := t.TempDir()

	one := &Package{Name: "gamma", SizeMax: "1", ListMax: "1"}
	two := &Package{Name: "Gamma", SizeMax: "1", ListMax: "1"}
	_, err := PlanGo(dir, Packages{one, two})
	if err == nil || !strings.Contains(err.Error(), "letter case") {
		t.Fatalf("got %v, want a letter case conflict", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("rejected plan wrote to disk: %v", entries)
	}
}

func TestPlanGoIdentConflicts(t *testing.T) {
	tests := []struct {
		name string
		make func() Packages
		want string
	}{
		{"field method clash", func() Packages {
			p := &Package{Name: "a", SizeMax: "1", ListMax: "1"}
			s := &Struct{Name: "o", Pkg: p}
			s.Fields = []*Field{{Name: "marshal_to", Struct: s, Type: "uint32"}}
			p.Structs = []*Struct{s}
			return Packages{p}
		}, "MarshalTo"},
		{"field case fold", func() Packages {
			p := &Package{Name: "a", SizeMax: "1", ListMax: "1"}
			s := &Struct{Name: "o", Pkg: p}
			s.Fields = []*Field{
				{Name: "foo_bar", Struct: s, Type: "uint32"},
				{Name: "fooBar", Struct: s, Type: "uint32"},
			}
			p.Structs = []*Struct{s}
			return Packages{p}
		}, "FooBar"},
		{"struct reserved", func() Packages {
			p := &Package{Name: "a", SizeMax: "1", ListMax: "1"}
			p.Structs = []*Struct{{Name: "colfer_max", Pkg: p}}
			return Packages{p}
		}, "ColferMax"},
		{"struct case fold", func() Packages {
			p := &Package{Name: "a", SizeMax: "1", ListMax: "1"}
			p.Structs = []*Struct{{Name: "a_b", Pkg: p}, {Name: "aB", Pkg: p}}
			return Packages{p}
		}, "AB"},
		{"package import clash", func() Packages {
			p := &Package{Name: "fmt", SizeMax: "1", ListMax: "1"}
			p.Structs = []*Struct{{Name: "o", Pkg: p}}
			return Packages{p}
		}, "clashes with import"},
		{"reference import clash", func() Packages {
			p1 := &Package{Name: "time", SizeMax: "1", ListMax: "1"}
			s1 := &Struct{Name: "o", Pkg: p1}
			p1.Structs = []*Struct{s1}
			p2 := &Package{Name: "a", SizeMax: "1", ListMax: "1"}
			s2 := &Struct{Name: "o", Pkg: p2}
			s2.Fields = []*Field{
				{Name: "ts", Struct: s2, Type: "timestamp"},
				{Name: "ref", Struct: s2, Type: "o", TypeRef: s1},
			}
			p2.Structs = []*Struct{s2}
			return Packages{p1, p2}
		}, "clashes with import"},
		{"reference alias clash", func() Packages {
			p1 := &Package{Name: "x/dup", SizeMax: "1", ListMax: "1"}
			s1 := &Struct{Name: "o", Pkg: p1}
			p1.Structs = []*Struct{s1}
			p2 := &Package{Name: "y/dup", SizeMax: "1", ListMax: "1"}
			s2 := &Struct{Name: "o", Pkg: p2}
			p2.Structs = []*Struct{s2}
			p3 := &Package{Name: "a", SizeMax: "1", ListMax: "1"}
			s3 := &Struct{Name: "o", Pkg: p3}
			s3.Fields = []*Field{
				{Name: "r1", Struct: s3, Type: "o", TypeRef: s1},
				{Name: "r2", Struct: s3, Type: "o", TypeRef: s2},
			}
			p3.Structs = []*Struct{s3}
			return Packages{p1, p2, p3}
		}, "same Go name"},
		{"invalid identifier", func() Packages {
			p := &Package{Name: "x/foo-bar", SizeMax: "1", ListMax: "1"}
			p.Structs = []*Struct{{Name: "o", Pkg: p}}
			return Packages{p}
		}, "invalid Go identifier"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := PlanGo(t.TempDir(), test.make())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want an error containing %q", err, test.want)
			}
		})
	}
}

func TestPlanGoOutsideBase(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/m\n"), 0666); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dir, "in")

	p := &Package{Name: "example.com/m/out", SizeMax: "1", ListMax: "1"}
	p.Structs = []*Struct{{Name: "o", Pkg: p}}
	_, err := PlanGo(base, Packages{p})
	if err == nil || !strings.Contains(err.Error(), "outside base directory") {
		t.Fatalf("got %v, want an outside base directory error", err)
	}
}

func TestGoPlanCommit(t *testing.T) {
	dir := t.TempDir()
	plan, err := PlanGo(dir, planTestPackages())
	if err != nil {
		t.Fatal("plan:", err)
	}
	if err := plan.Commit(); err != nil {
		t.Fatal("commit:", err)
	}

	for _, f := range plan.Files {
		content, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Errorf("read %q: %v", f.Path, err)
			continue
		}
		if !bytes.Equal(content, f.Data) {
			t.Errorf("file %q content differs from the plan", f.Path)
		}
	}
	assertNoStaging(t, dir)

	// regeneration of the same schema commits cleanly
	again, err := PlanGo(dir, planTestPackages())
	if err != nil {
		t.Fatal("replan:", err)
	}
	if err := again.Commit(); err != nil {
		t.Fatal("second commit:", err)
	}
}

func TestGoPlanCommitExternalChange(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateGo(dir, planTestPackages()); err != nil {
		t.Fatal("generate:", err)
	}

	plan, err := PlanGo(dir, planTestPackages())
	if err != nil {
		t.Fatal("plan:", err)
	}

	// external modification after the plan was made
	target := filepath.Join(dir, filepath.FromSlash(plan.Files[0].Path))
	evil := []byte("package hacked\n")
	if err := os.WriteFile(target, evil, 0666); err != nil {
		t.Fatal(err)
	}

	err = plan.Commit()
	if err == nil || !strings.Contains(err.Error(), "changed since the plan was made") {
		t.Fatalf("got %v, want a checksum rejection", err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, evil) {
		t.Error("externally modified file was overwritten")
	}
	assertNoStaging(t, dir)
}

// faultFS injects a file operation failure.
type faultFS struct {
	fileOps
	failOp string // operation to fail
	failAt int    // call number to fail, 1-based
	calls  int
}

var errInjected = errors.New("injected file operation failure")

func (f *faultFS) fail(op string) error {
	if op != f.failOp {
		return nil
	}
	f.calls++
	if f.calls == f.failAt {
		return errInjected
	}
	return nil
}

func (f *faultFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	if err := f.fail("WriteFile"); err != nil {
		return err
	}
	return f.fileOps.WriteFile(name, data, perm)
}

func (f *faultFS) Rename(oldpath, newpath string) error {
	if err := f.fail("Rename"); err != nil {
		return err
	}
	return f.fileOps.Rename(oldpath, newpath)
}

func (f *faultFS) MkdirAll(path string, perm os.FileMode) error {
	if err := f.fail("MkdirAll"); err != nil {
		return err
	}
	return f.fileOps.MkdirAll(path, perm)
}

func TestGoPlanCommitFault(t *testing.T) {
	const original = "// original content\n"

	// fail each file operation in turn
	for _, op := range []string{"WriteFile", "MkdirAll", "Rename"} {
		for failAt := 1; failAt <= 6; failAt++ {
			t.Run(fmt.Sprintf("%s-%d", op, failAt), func(t *testing.T) {
				dir := t.TempDir()

				// pre-existing code for one of the packages
				pre := filepath.Join(dir, "alpha", "Colfer.go")
				if err := os.MkdirAll(filepath.Dir(pre), 0777); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(pre, []byte(original), 0666); err != nil {
					t.Fatal(err)
				}

				plan, err := PlanGo(dir, planTestPackages())
				if err != nil {
					t.Fatal("plan:", err)
				}

				planFS = &faultFS{fileOps: osFileOps{}, failOp: op, failAt: failAt}
				commitErr := plan.Commit()
				planFS = osFileOps{}
				if commitErr == nil {
					// failure injected after the last critical operation
					return
				}
				if !errors.Is(commitErr, errInjected) {
					t.Fatalf("got %v, want the injected failure", commitErr)
				}

				// no mix of old and new code
				for _, f := range plan.Files {
					target := filepath.Join(dir, filepath.FromSlash(f.Path))
					content, err := os.ReadFile(target)
					switch {
					case os.IsNotExist(err):
						if f.Path == "alpha/Colfer.go" {
							t.Errorf("pre-existing file %q lost", f.Path)
						}
					case err != nil:
						t.Error(err)
					case f.Path == "alpha/Colfer.go" && string(content) != original:
						t.Errorf("file %q not restored to the original content", f.Path)
					case f.Path != "alpha/Colfer.go":
						t.Errorf("new file %q left behind", f.Path)
					}
				}
				assertNoStaging(t, dir)
			})
		}
	}
}

func TestGoPlanCommitPathGuard(t *testing.T) {
	dir := t.TempDir()

	for _, path := range []string{"../evil.go", "/evil.go", "a/../../evil.go", ""} {
		plan := &GoPlan{
			Basedir: dir,
			Files:   []*GoFile{{Path: path, Data: []byte("package evil\n")}},
		}
		if err := plan.Commit(); err == nil {
			t.Errorf("commit of path %q succeeded", path)
		}
		if _, err := os.Stat(filepath.Join(dir, "..", "evil.go")); !os.IsNotExist(err) {
			t.Errorf("commit of path %q escaped the base directory", path)
		}
	}

	empty := &GoPlan{Basedir: dir}
	if err := empty.Commit(); err == nil {
		t.Error("commit of empty plan succeeded")
	}
}

func assertNoStaging(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".colfer-plan-") {
			t.Errorf("staging directory %q left behind", e.Name())
		}
	}
}
