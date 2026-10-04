package colfer

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"go/format"
	"go/token"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/pascaldekloe/name"
)

// GoFile is one output file of a GoPlan.
type GoFile struct {
	// Path is the target location relative to GoPlan.Basedir,
	// using slashes as the separator.
	Path string
	// Data is the generated file content.
	Data []byte

	// existed flags whether the target was present at plan time.
	// Digest is the SHA-256 checksum of the pre-existing content.
	existed bool
	digest  [sha256.Size]byte
}

// GoPlan is a pending Go code generation. The plan lists each
// target file with its full content for review. Nothing is
// written to disk before Commit.
type GoPlan struct {
	// Basedir is the root directory for the generated code.
	Basedir string
	// Files are the output targets sorted by Path.
	Files []*GoFile
}

// fileOps abstracts the file system for plan commits.
type fileOps interface {
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	MkdirTemp(dir, pattern string) (string, error)
	Rename(oldpath, newpath string) error
	Remove(name string) error
	RemoveAll(path string) error
}

type osFileOps struct{}

func (osFileOps) ReadFile(name string) ([]byte, error) { return ioutil.ReadFile(name) }
func (osFileOps) WriteFile(name string, data []byte, perm os.FileMode) error {
	return ioutil.WriteFile(name, data, perm)
}
func (osFileOps) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (osFileOps) MkdirTemp(dir, pattern string) (string, error) {
	return os.MkdirTemp(dir, pattern)
}
func (osFileOps) Rename(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }
func (osFileOps) Remove(name string) error             { return os.Remove(name) }
func (osFileOps) RemoveAll(path string) error          { return os.RemoveAll(path) }

// planFS is the file system used by GoPlan. Tests replace the
// implementation to inject file operation failures.
var planFS fileOps = osFileOps{}

// goPackageIdents are the package-level identifiers in generated Go code.
var goPackageIdents = []string{
	"intconv", "ColferSizeMax", "ColferListMax",
	"ColferMax", "ColferError", "ColferTail",
}

// goMethodIdents are the method names on generated Go structs.
var goMethodIdents = []string{
	"MarshalTo", "MarshalLen", "MarshalBinary",
	"Unmarshal", "UnmarshalBinary",
}

// PlanGo prepares the Go code generation for packages. The plan
// resolves the Go identifiers, the import aliases and the target
// paths, including conflicts after letter case folding. Existing
// target files are read for a checksum, which protects Commit
// against external modifications. No files are created, modified
// or deleted. All targets are confined to basedir.
func PlanGo(basedir string, packages Packages) (*GoPlan, error) {
	if len(packages) == 0 {
		return nil, fmt.Errorf("colfer: no packages to generate")
	}
	if err := goIdents(packages); err != nil {
		return nil, err
	}

	t := template.New("go-code")
	template.Must(t.Parse(goCode))
	template.Must(t.New("marshal-field").Parse(goMarshalField))
	template.Must(t.New("marshal-field-len").Parse(goMarshalFieldLen))
	template.Must(t.New("unmarshal-field").Parse(goUnmarshalField))
	template.Must(t.New("unmarshal-varint").Parse(goUnmarshalVarint))

	modDir, modPkg, err := goMod(basedir)
	if err != nil {
		return nil, err
	}
	absBase, err := filepath.Abs(basedir)
	if err != nil {
		return nil, err
	}

	plan := &GoPlan{Basedir: basedir}
	seen := make(map[string]string, len(packages)) // case folded path → path
	for _, p := range packages {
		var buf bytes.Buffer
		if err := t.Execute(&buf, p); err != nil {
			return nil, err
		}
		clean, err := format.Source(buf.Bytes())
		if err != nil {
			return nil, fmt.Errorf("colfer: format package %q: %s", p.Name, err)
		}

		dir := filepath.Join(basedir, p.Name)
		if modPkg != "" && strings.HasPrefix(p.Name, modPkg+"/") {
			dir = filepath.Join(modDir, p.Name[len(modPkg):])
		}
		target, err := filepath.Abs(filepath.Join(dir, "Colfer.go"))
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(absBase, target)
		if err != nil {
			return nil, err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("colfer: package %q targets %q outside base directory %q", p.Name, target, basedir)
		}
		slash := filepath.ToSlash(rel)
		if prev, ok := seen[strings.ToLower(slash)]; ok {
			if prev == slash {
				return nil, fmt.Errorf("colfer: packages target the same file %q", slash)
			}
			return nil, fmt.Errorf("colfer: target files %q and %q differ in letter case only", prev, slash)
		}
		seen[strings.ToLower(slash)] = slash

		f := &GoFile{Path: slash, Data: clean}
		switch content, err := planFS.ReadFile(target); {
		case err == nil:
			f.existed = true
			f.digest = sha256.Sum256(content)
		case os.IsNotExist(err):
			// new file
		default:
			return nil, err
		}
		plan.Files = append(plan.Files, f)
	}

	sort.Slice(plan.Files, func(i, j int) bool { return plan.Files[i].Path < plan.Files[j].Path })
	return plan, nil
}

// goIdents derives the native Go identifiers and verifies that the
// generated code is free of identifier and import alias conflicts.
func goIdents(packages Packages) error {
	for _, p := range packages {
		p.NameNative = p.Name[strings.LastIndexByte(p.Name, '/')+1:]
		if !token.IsIdentifier(p.NameNative) {
			return fmt.Errorf("colfer: package %q maps to invalid Go identifier %q", p.Name, p.NameNative)
		}
	}

	for _, p := range packages {
		imports := map[string]string{
			"binary": "encoding/binary",
			"fmt":    "fmt",
			"io":     "io",
		}
		if p.HasFloat() {
			imports["math"] = "math"
		}
		if p.HasTimestamp() {
			imports["time"] = "time"
		}

		if path, ok := imports[p.NameNative]; ok {
			return fmt.Errorf("colfer: package %q Go name %q clashes with import %q", p.Name, p.NameNative, path)
		}

		aliases := make(map[string]string) // native name → package name
		for _, r := range p.Refs() {
			if path, ok := imports[r.NameNative]; ok {
				return fmt.Errorf("colfer: package %q reference %q Go name %q clashes with import %q", p.Name, r.Name, r.NameNative, path)
			}
			if prev, ok := aliases[r.NameNative]; ok {
				return fmt.Errorf("colfer: package %q imports %q and %q under the same Go name %q", p.Name, prev, r.Name, r.NameNative)
			}
			aliases[r.NameNative] = r.Name
		}
	}

	for _, p := range packages {
		structNames := make(map[string]string, len(p.Structs))
		for _, t := range p.Structs {
			t.NameNative = name.CamelCase(t.Name, true)
			for _, ident := range goPackageIdents {
				if t.NameNative == ident {
					return fmt.Errorf("colfer: struct %s maps to reserved Go identifier %q", t, ident)
				}
			}
			if prev, ok := structNames[t.NameNative]; ok {
				return fmt.Errorf("colfer: structs %q and %q in package %q both map to Go identifier %q", prev, t.Name, p.Name, t.NameNative)
			}
			structNames[t.NameNative] = t.Name

			fieldNames := make(map[string]string, len(t.Fields))
			for _, f := range t.Fields {
				f.NameNative = name.CamelCase(f.Name, true)
				for _, ident := range goMethodIdents {
					if f.NameNative == ident {
						return fmt.Errorf("colfer: field %s maps to generated Go method %q", f, ident)
					}
				}
				if prev, ok := fieldNames[f.NameNative]; ok {
					return fmt.Errorf("colfer: fields %q and %q in struct %s both map to Go identifier %q", prev, f.Name, t, f.NameNative)
				}
				fieldNames[f.NameNative] = f.Name
			}
		}
	}

	for _, p := range packages {
		for _, t := range p.Structs {
			for _, f := range t.Fields {
				switch f.Type {
				default:
					if f.TypeRef == nil {
						f.TypeNative = f.Type
					} else {
						f.TypeNative = f.TypeRef.NameNative
						if f.TypeRef.Pkg != p {
							f.TypeNative = f.TypeRef.Pkg.NameNative + "." + f.TypeNative
						}
					}
				case "timestamp":
					f.TypeNative = "time.Time"
				case "text":
					f.TypeNative = "string"
				case "binary":
					f.TypeNative = "[]byte"
				}
			}
		}
	}
	return nil
}

// Commit writes the plan to its base directory. Only the relative
// paths listed in the plan are written. Content is staged in a
// temporary directory within the base directory first, so all file
// operations stay on the same file system of a single machine; the
// commit is not a cross-file-system atomic transaction. Pre-existing
// files are replaced only when their content still matches the
// checksum taken at plan time. A failure restores the original files
// and removes the staging directory, leaving no mix of old and new
// code behind.
func (p *GoPlan) Commit() error {
	if len(p.Files) == 0 {
		return fmt.Errorf("colfer: commit of empty Go plan")
	}
	absBase, err := filepath.Abs(p.Basedir)
	if err != nil {
		return err
	}

	targets := make([]string, len(p.Files))
	for i, f := range p.Files {
		target, err := planTarget(absBase, f.Path)
		if err != nil {
			return err
		}
		targets[i] = target
	}

	// verify pre-existing content is untouched
	for i, f := range p.Files {
		content, err := planFS.ReadFile(targets[i])
		switch {
		case os.IsNotExist(err):
			if f.existed {
				return fmt.Errorf("colfer: file %q vanished since the plan was made", f.Path)
			}
		case err != nil:
			return err
		case !f.existed:
			return fmt.Errorf("colfer: file %q appeared since the plan was made", f.Path)
		case sha256.Sum256(content) != f.digest:
			return fmt.Errorf("colfer: file %q changed since the plan was made", f.Path)
		}
	}

	if err := planFS.MkdirAll(absBase, 0777); err != nil {
		return err
	}

	// stage all content in a single directory next to the targets
	staging, err := planFS.MkdirTemp(absBase, ".colfer-plan-")
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			planFS.RemoveAll(staging)
		}
	}()

	staged := make([]string, len(p.Files))
	for i, f := range p.Files {
		staged[i] = filepath.Join(staging, fmt.Sprintf("staged-%d", i))
		if err := planFS.WriteFile(staged[i], f.Data, 0666); err != nil {
			return err
		}
	}

	// install with backups of the pre-existing files
	type install struct {
		target, backup string
	}
	installed := make([]install, 0, len(p.Files))
	rollback := func() {
		for i := len(installed) - 1; i >= 0; i-- {
			planFS.Remove(installed[i].target)
			if installed[i].backup != "" {
				planFS.Rename(installed[i].backup, installed[i].target)
			}
		}
	}

	for i, f := range p.Files {
		if err := planFS.MkdirAll(filepath.Dir(targets[i]), 0777); err != nil {
			rollback()
			return err
		}

		var backup string
		if f.existed {
			backup = filepath.Join(staging, fmt.Sprintf("backup-%d", i))
			if err := planFS.Rename(targets[i], backup); err != nil {
				rollback()
				return err
			}
		}
		if err := planFS.Rename(staged[i], targets[i]); err != nil {
			if backup != "" {
				planFS.Rename(backup, targets[i])
			}
			rollback()
			return err
		}
		installed = append(installed, install{targets[i], backup})
	}

	complete = true
	return planFS.RemoveAll(staging)
}

// planTarget resolves a plan path against the base directory and
// guards against escapes.
func planTarget(absBase, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("colfer: plan path %q is not relative", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("colfer: plan path %q escapes the base directory", rel)
	}
	target := filepath.Join(absBase, clean)
	if !strings.HasPrefix(target, absBase+string(filepath.Separator)) {
		return "", fmt.Errorf("colfer: plan path %q escapes the base directory", rel)
	}
	return target, nil
}
