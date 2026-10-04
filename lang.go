package colfer

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"go/format"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"
	"unicode/utf8"

	"github.com/pascaldekloe/name"
	"golang.org/x/mod/modfile"
)

// CKeywords are the reserved tokens for C code.
// Some entries are redundant due to the use of a Go parser.
var cKeywords = map[string]struct{}{
	"auto": {}, "break": {}, "case": {}, "char": {},
	"const": {}, "continue": {}, "default": {}, "do": {},
	"double": {}, "else": {}, "enum": {}, "extern": {},
	"float": {}, "for": {}, "goto": {}, "if": {},
	"int": {}, "long": {}, "register": {}, "return": {},
	"short": {}, "signed": {}, "sizeof": {}, "static": {},
	"struct": {}, "switch": {}, "typedef": {}, "union": {},
	"unsigned": {}, "void": {}, "volatile": {}, "while": {},
}

// GenerateC writes the code into file "Colfer.h" and "Colfer.c".
func GenerateC(basedir string, packages Packages) error {
	for _, p := range packages {
		for _, t := range p.Structs {
			t.NameNative = strings.ToLower(name.SnakeCase(p.Name + "_" + t.Name))

			for _, f := range t.Fields {
				f.NameNative = strings.ToLower(name.SnakeCase(f.Name))
				if _, ok := cKeywords[f.NameNative]; ok {
					f.NameNative += "_"
				}

				switch f.Type {
				case "bool":
					f.TypeNative = "char"
				case "uint8", "uint16", "uint32", "uint64", "int32", "int64":
					f.TypeNative = f.Type + "_t"
				case "float32":
					f.TypeNative = "float"
				case "float64":
					f.TypeNative = "double"
				case "timestamp":
					f.TypeNative = "timespec"
				case "binary", "text":
					f.TypeNative = "colfer_" + f.Type
				}
			}
		}
	}

	if err := os.MkdirAll(basedir, os.ModeDir|os.ModePerm); err != nil {
		return err
	}

	f, err := os.Create(filepath.Join(basedir, "Colfer.h"))
	if err != nil {
		return err
	}
	if err := template.Must(template.New("C-header").Parse(cHeaderTemplate)).Execute(f, packages); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}

	f, err = os.Create(filepath.Join(basedir, "Colfer.c"))
	if err != nil {
		return err
	}
	if err := template.Must(template.New("C").Parse(cTemplate)).Execute(f, packages); err != nil {
		return err
	}
	return f.Close()
}

//go:embed template/c-header.txt
var cHeaderTemplate string

//go:embed template/c.txt
var cTemplate string

// ECMAKeywords are the reserved tokens for ECMA Script.
// Some entries are redundant due to the use of a Go parser.
var eCMAKeywords = map[string]struct{}{
	"break": {}, "case": {}, "catch": {}, "class": {},
	"const": {}, "continue": {}, "debugger": {}, "default": {},
	"delete": {}, "do": {}, "else": {}, "enum": {},
	"export": {}, "extends": {}, "finally": {}, "for": {},
	"function": {}, "if": {}, "import": {}, "in": {},
	"instanceof": {}, "new": {}, "return": {}, "super": {},
	"switch": {}, "this": {}, "throw": {}, "try": {},
	"typeof": {}, "var": {}, "void": {}, "while": {},
	"with": {}, "yield": {},
}

// GenerateECMA writes the code into file "Colfer.js".
func GenerateECMA(basedir string, packages Packages) error {
	for _, p := range packages {
		p.NameNative = strings.Replace(p.Name, "/", "_", -1)
		if _, ok := eCMAKeywords[p.NameNative]; ok {
			p.NameNative += "_"
		}

		for _, t := range p.Structs {
			t.NameNative = name.CamelCase(t.Name, true)
			for _, f := range t.Fields {
				f.NameNative = name.CamelCase(f.Name, false)
				if _, ok := eCMAKeywords[f.NameNative]; ok {
					f.NameNative += "_"
				}
			}
		}
	}

	t := template.New("ecma-code")
	template.Must(t.Parse(ecmaCode))
	template.Must(t.New("marshal").Parse(ecmaMarshal))
	template.Must(t.New("unmarshal").Parse(ecmaUnmarshal))

	if err := os.MkdirAll(basedir, os.ModeDir|os.ModePerm); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(basedir, "Colfer.js"))
	if err != nil {
		return err
	}
	defer f.Close()
	return t.Execute(f, packages)
}

//go:embed template/ecma.txt
var ecmaCode string

//go:embed template/ecma-marshal.txt
var ecmaMarshal string

//go:embed template/ecma-unmarshal.txt
var ecmaUnmarshal string

// GoMod looks for a Go modules definition.
// ModDir is the root directory when found.
// ModPkg is the root package when found.
func goMod(dir string) (modDir, modPkg string, err error) {
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}

	for n := 0; n < 32; n++ {
		path := filepath.Join(dir, "go.mod")
		text, err := ioutil.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return "", "", err
			}

			// If it is the root directory, break
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}

			// try parent directory
			dir = parent
			continue
		}

		return dir, modfile.ModulePath(text), nil
	}

	return "", "", nil // not found
}

// GoImport is an import path with an optional alias for generated Go code.
type GoImport struct {
	// Path is the import path.
	Path string
	// Qual is the qualifier used in the generated code.
	Qual string
	// Alias flags whether Qual differs from the package name,
	// i.e. the import requires an explicit alias.
	Alias bool
}

// goIdentsReserved are the package-level identifiers in generated Go code.
var goIdentsReserved = []string{
	"intconv", "ColferSizeMax", "ColferListMax",
	"ColferMax", "ColferError", "ColferTail",
}

// goMethodsReserved are the method names in generated Go code.
var goMethodsReserved = []string{
	"MarshalTo", "MarshalLen", "MarshalBinary",
	"Unmarshal", "UnmarshalBinary",
}

// GenerateGo writes the code into file "Colfer.go".
func GenerateGo(basedir string, packages Packages) error {
	plan, err := PlanGo(basedir, packages)
	if err != nil {
		return err
	}
	if len(plan.Files) == 0 {
		return nil
	}
	return plan.Commit(basedir)
}

// PlanGo renders the Go code for packages into a Plan. The basedir is
// the commit target; it is used for go.mod resolution and for the file
// paths in the Plan. PlanGo does not write to the filesystem. The
// returned Plan is deterministic: the same schema input always produces
// the same file content, regardless of map iteration order or previous
// calls.
func PlanGo(basedir string, packages Packages) (*Plan, error) {
	t := template.New("go-code")
	template.Must(t.Parse(goCode))
	template.Must(t.New("marshal-field").Parse(goMarshalField))
	template.Must(t.New("marshal-field-len").Parse(goMarshalFieldLen))
	template.Must(t.New("unmarshal-field").Parse(goUnmarshalField))
	template.Must(t.New("unmarshal-varint").Parse(goUnmarshalVarint))

	absBase, err := filepath.Abs(basedir)
	if err != nil {
		return nil, err
	}
	modDir, modPkg, err := goMod(basedir)
	if err != nil {
		return nil, err
	}

	for _, p := range packages {
		p.NameNative = p.Name[strings.LastIndexByte(p.Name, '/')+1:]
		for _, t := range p.Structs {
			t.NameNative = name.CamelCase(t.Name, true)
			for _, f := range t.Fields {
				f.NameNative = name.CamelCase(f.Name, true)
			}
		}
	}
	if err := checkGoIdents(packages); err != nil {
		return nil, err
	}

	plan := new(Plan)
	fold := make(map[string]string, len(packages))
	for _, p := range packages {
		quals := planGoImports(p)

		for _, t := range p.Structs {
			for _, f := range t.Fields {
				switch f.Type {
				default:
					if f.TypeRef == nil {
						f.TypeNative = f.Type
					} else {
						f.TypeNative = f.TypeRef.NameNative
						if f.TypeRef.Pkg != p {
							f.TypeNative = quals[f.TypeRef.Pkg] + "." + f.TypeNative
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

		var buf bytes.Buffer
		if err := t.Execute(&buf, p); err != nil {
			return nil, err
		}
		content, err := format.Source(buf.Bytes())
		if err != nil {
			return nil, fmt.Errorf("colfer: generated Go for package %s: %s", p.Name, err)
		}

		target := filepath.Join(absBase, filepath.FromSlash(p.Name))
		if modPkg != "" && strings.HasPrefix(p.Name, modPkg+"/") {
			target = filepath.Join(modDir, filepath.FromSlash(p.Name[len(modPkg):]))
		}
		target = filepath.Join(target, "Colfer.go")
		rel, err := filepath.Rel(absBase, target)
		if err != nil {
			return nil, err
		}
		relSlash := filepath.ToSlash(rel)
		if !validPlanPath(relSlash) {
			return nil, fmt.Errorf("colfer: package %s target %q is not within the base directory %q", p.Name, target, basedir)
		}
		if prev, ok := fold[strings.ToUpper(relSlash)]; ok {
			return nil, fmt.Errorf("colfer: target paths %q and %q conflict on case-insensitive filesystems", prev, relSlash)
		}
		fold[strings.ToUpper(relSlash)] = relSlash

		f, err := plan.newFile(relSlash, content)
		if err != nil {
			return nil, err
		}
		switch prev, err := osReadFile(target); {
		case os.IsNotExist(err):
			// new file
		case err != nil:
			return nil, err
		default:
			f.replace = true
			f.previous = sha256.Sum256(prev)
		}
	}
	plan.sortFiles()
	return plan, nil
}

// checkGoIdents verifies that the native Go identifiers are unique
// within their respective scope and free of conflicts with the
// generated code.
func checkGoIdents(packages Packages) error {
	for _, p := range packages {
		structs := make(map[string]*Struct, len(p.Structs))
		for _, t := range p.Structs {
			for _, reserved := range goIdentsReserved {
				if t.NameNative == reserved {
					return fmt.Errorf("colfer: struct %s maps to Go identifier %s, which is reserved by the generated code", t, reserved)
				}
			}
			if dupe, ok := structs[t.NameNative]; ok {
				return fmt.Errorf("colfer: structs %s and %s both map to Go identifier %s", dupe, t, t.NameNative)
			}
			structs[t.NameNative] = t

			fields := make(map[string]*Field, len(t.Fields))
			for _, f := range t.Fields {
				for _, reserved := range goMethodsReserved {
					if f.NameNative == reserved {
						return fmt.Errorf("colfer: field %s maps to Go identifier %s, which is reserved by the generated code", f, reserved)
					}
				}
				if dupe, ok := fields[f.NameNative]; ok {
					return fmt.Errorf("colfer: fields %s and %s both map to Go identifier %s", dupe, f, f.NameNative)
				}
				fields[f.NameNative] = f
			}
		}
	}
	return nil
}

// planGoImports sets the Go import aliases for p and returns the
// qualifier per referenced package. Aliases are assigned in sorted
// import-path order to keep the output deterministic.
func planGoImports(p *Package) map[*Package]string {
	refs := p.Refs()
	used := make(map[string]struct{}, len(refs)+len(p.Structs)+len(goIdentsReserved))
	for _, ident := range goIdentsReserved {
		used[ident] = struct{}{}
	}
	for _, t := range p.Structs {
		used[t.NameNative] = struct{}{}
	}

	imports := make([]GoImport, 0, len(refs))
	quals := make(map[*Package]string, len(refs))
	for _, r := range refs {
		qual := r.NameNative
		if _, taken := used[qual]; taken {
			for n := 2; ; n++ {
				candidate := fmt.Sprintf("%s%d", r.NameNative, n)
				if _, taken := used[candidate]; !taken {
					qual = candidate
					break
				}
			}
		}
		used[qual] = struct{}{}
		quals[r] = qual
		imports = append(imports, GoImport{Path: r.Name, Qual: qual, Alias: qual != r.NameNative})
	}
	p.GoImports = imports
	return quals
}

//go:embed template/go.txt
var goCode string

//go:embed template/go-marshal-field.txt
var goMarshalField string

//go:embed template/go-marshal-field-len.txt
var goMarshalFieldLen string

//go:embed template/go-unmarshal-field.txt
var goUnmarshalField string

//go:embed template/go-unmarshal-varint.txt
var goUnmarshalVarint string

// JavaKeywords are the reserved tokens for Java code.
// Some entries are redundant due to the use of a Go parser.
var javaKeywords = map[string]struct{}{
	"abstract": {}, "assert": {}, "boolean": {}, "break": {},
	"byte": {}, "case": {}, "catch": {}, "char": {},
	"class": {}, "const": {}, "continue": {}, "default": {},
	"do": {}, "double": {}, "else": {}, "enum": {},
	"extends": {}, "final": {}, "finally": {}, "float": {},
	"for": {}, "goto": {}, "if": {}, "implements": {},
	"import": {}, "instanceof": {}, "int": {}, "interface": {},
	"long": {}, "native": {}, "new": {}, "package": {},
	"private": {}, "protected": {}, "public": {}, "return": {},
	"short": {}, "static": {}, "strictfp": {}, "super": {},
	"switch": {}, "synchronized": {}, "this": {}, "throw": {},
	"throws": {}, "transient": {}, "try": {}, "void": {},
	"volatile": {}, "while": {},
}

func toJavaName(name string) string {
	name = strings.ReplaceAll(name, "/", ".")

	segments := strings.Split(name, ".")
	var escapes bool
	for i, s := range segments {
		if _, ok := javaKeywords[s]; ok {
			segments[i] = s + "_"
			escapes = true
		}
	}
	if escapes {
		return strings.Join(segments, ".")
	}
	return name
}

// GenerateJava writes the code into the respective ".java" files.
func GenerateJava(basedir string, packages Packages) error {
	titleCache := make(map[string]string)
	funcs := template.FuncMap{"title": func(s string) string {
		if t, ok := titleCache[s]; ok {
			return t
		}

		r, size := utf8.DecodeRuneInString(s)
		if size == 0 {
			return s
		}
		t := string([]rune{unicode.ToUpper(r)}) + s[size:]

		titleCache[s] = t
		return t
	}}

	packageTemplate := template.New("java-package")
	template.Must(packageTemplate.Parse(javaPackage))
	codeTemplate := template.New("java-code").Funcs(funcs)
	template.Must(codeTemplate.Parse(javaCode))

	for _, p := range packages {
		p.NameNative = toJavaName(p.Name)
		p.SuperClassNative = toJavaName(p.SuperClass)

		p.InterfaceNatives = make([]string, len(p.Interfaces))
		for i, s := range p.Interfaces {
			p.InterfaceNatives[i] = toJavaName(s)
		}

		for _, t := range p.Structs {
			t.NameNative = name.CamelCase(t.Name, true)
			for _, f := range t.Fields {
				f.NameNative = name.CamelCase(f.Name, false)
				if _, ok := javaKeywords[f.NameNative]; ok {
					f.NameNative += "_"
				}
			}
		}
	}

	for _, p := range packages {
		pkgdir := filepath.Join(basedir, strings.Replace(p.NameNative, ".", string([]rune{filepath.Separator}), -1))
		if err := os.MkdirAll(pkgdir, os.ModeDir|os.ModePerm); err != nil {
			return err
		}

		if doc := p.DocText(" * "); doc != "" {
			f, err := os.Create(filepath.Join(pkgdir, "package-info.java"))
			if err != nil {
				return err
			}
			defer f.Close()

			if err := packageTemplate.Execute(f, p); err != nil {
				return err
			}
		}

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
				case "bool":
					f.TypeNative = "boolean"
				case "uint8":
					f.TypeNative = "byte"
				case "uint16":
					f.TypeNative = "short"
				case "uint32", "int32":
					f.TypeNative = "int"
				case "uint64", "int64":
					f.TypeNative = "long"
				case "float32":
					f.TypeNative = "float"
				case "float64":
					f.TypeNative = "double"
				case "timestamp":
					f.TypeNative = "java.time.Instant"
				case "text":
					f.TypeNative = "String"
				case "binary":
					f.TypeNative = "byte[]"
				}
			}

			f, err := os.Create(filepath.Join(pkgdir, t.NameNative+".java"))
			if err != nil {
				return err
			}
			defer f.Close()

			if err := codeTemplate.Execute(f, t); err != nil {
				return err
			}
		}
	}
	return nil
}

//go:embed template/java-package.txt
var javaPackage string

//go:embed template/java.txt
var javaCode string
