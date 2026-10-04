package colfer

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// crossCase defines one schema evolution and the fixed-byte expectations for
// each reading direction against the actual generated Go codec.
type crossCase struct {
	name    string
	oldFile string
	newFile string
	typ     string // exported Go type name to decode into (both revisions)

	// Size bounds passed to the generator per revision.
	oldSizeMax string
	newSizeMax string

	// expectOldToNew / expectNewToOld state what cross-reading fixed bytes
	// must show. Each entry is a fixed old/new serial plus the expected
	// outcome on the opposite decoder.
	oldToNew []crossExpect
	newToOld []crossExpect

	// wantVerdicts pins the static audit conclusion per direction.
	wantOldToNew Verdict
	wantNewToOld Verdict
}

type crossExpect struct {
	serialHex string // fixed writer bytes (whole message incl. 0x7f)
	ok        bool   // whether the opposite decoder must accept the bytes
	errSubstr string // when !ok, substring required in the error
}

func crossCases() []crossCase {
	return []crossCase{
		{
			name:         "append",
			oldFile:      "append.old.colf",
			newFile:      "append.new.colf",
			typ:          "Rec",
			oldSizeMax:   "1024",
			newSizeMax:   "1024",
			wantOldToNew: VerdictCompatible,
			wantNewToOld: VerdictIncompatible,
			// Old writer emits only field 0; new reader accepts.
			oldToNew: []crossExpect{
				{serialHex: "00017f", ok: true},
			},
			// New writer sets appended field 1; old reader hits unknown header.
			newToOld: []crossExpect{
				{serialHex: "01017f", ok: false, errSubstr: "unknown header"},
			},
		},
		{
			name:         "rename",
			oldFile:      "rename.old.colf",
			newFile:      "rename.new.colf",
			typ:          "Rec",
			oldSizeMax:   "1024",
			newSizeMax:   "1024",
			wantOldToNew: VerdictCompatible,
			wantNewToOld: VerdictCompatible,
			oldToNew:     []crossExpect{{serialHex: "00017f", ok: true}},
			newToOld:     []crossExpect{{serialHex: "00017f", ok: true}},
		},
		{
			name:         "flip",
			oldFile:      "flip.old.colf",
			newFile:      "flip.new.colf",
			typ:          "Rec",
			oldSizeMax:   "1024",
			newSizeMax:   "1024",
			wantOldToNew: VerdictIncompatible,
			wantNewToOld: VerdictIncompatible,
			// Scalar float64 value 1.0: header 00 + 8 bytes + 0x7f.
			oldToNew: []crossExpect{
				{serialHex: "003ff00000000000007f", ok: false, errSubstr: "EOF"},
			},
			// List of one float64: header 00, count 01, then 8 bytes, 0x7f.
			newToOld: []crossExpect{
				{serialHex: "00013ff00000000000007f", ok: false, errSubstr: "unknown header"},
			},
		},
		{
			name:         "nested",
			oldFile:      "nested.old.colf",
			newFile:      "nested.new.colf",
			typ:          "Outer",
			oldSizeMax:   "1024",
			newSizeMax:   "1024",
			wantOldToNew: VerdictIncompatible,
			wantNewToOld: VerdictIncompatible,
			// outer 00, inner 80, uint32 value 1<<21 as 4 big-endian bytes,
			// inner 7f, outer 7f. The new uint64 reader takes header 00 as
			// the varint form and loses alignment (here: truncated read).
			oldToNew: []crossExpect{
				{serialHex: "0080002000007f7f", ok: false, errSubstr: "EOF"},
			},
			// New inner x is uint64. A value >= 1<<49 forces the 8-byte
			// fixed form: header 80 followed by 8 big-endian bytes. The old
			// uint32 reader instead treats header 00 as the varint form,
			// consumes the first fixed byte as the value and the next byte as
			// a header, losing alignment until it reports an unknown header.
			// outer 00, inner 80, 1<<49 as big endian, inner 7f, outer 7f.
			newToOld: []crossExpect{
				{serialHex: "008000000200000000007f7f", ok: false, errSubstr: "unknown header"},
			},
		},
		{
			name:         "sizemax",
			oldFile:      "sizemax.old.colf",
			newFile:      "sizemax.new.colf",
			typ:          "Rec",
			oldSizeMax:   "8",
			newSizeMax:   "4",
			wantOldToNew: VerdictIncompatible,
			wantNewToOld: VerdictCompatible,
			// 4-char text "ABCD": header 00, len 04, 4 bytes, 0x7f = 7 bytes.
			// Legal under old SizeMax 8; the new decoder cap 4 rejects it.
			oldToNew: []crossExpect{
				{serialHex: "0004414243447f", ok: false, errSubstr: "exceeds"},
			},
		},
	}
}

func TestCrossReadFixedBytes(t *testing.T) {
	if testing.Short() {
		t.Skip("cross codec generation in short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go toolchain unavailable: %s", err)
	}

	for _, tc := range crossCases() {
		t.Run(tc.name, func(t *testing.T) {
			runCrossCase(t, tc)
		})
	}
}

func runCrossCase(t *testing.T, tc crossCase) {
	root := t.TempDir()

	oldDir := filepath.Join(root, "old")
	newDir := filepath.Join(root, "new")
	for _, d := range []string{oldDir, newDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	oldPkgs := mustParseForCross(t, filepath.Join("testdata", "compat", tc.oldFile))
	newPkgs := mustParseForCross(t, filepath.Join("testdata", "compat", tc.newFile))

	setSizeMax(oldPkgs, tc.oldSizeMax)
	setSizeMax(newPkgs, tc.newSizeMax)

	// Static audit must match the wired verdict expectation.
	rep := Compare(oldPkgs, newPkgs,
		map[string]Limits{"gen": {SizeMax: tc.oldSizeMax, ListMax: "64 * 1024"}},
		map[string]Limits{"gen": {SizeMax: tc.newSizeMax, ListMax: "64 * 1024"}})
	if rep.OldToNew != tc.wantOldToNew {
		t.Errorf("audit old->new = %s, want %s", rep.OldToNew, tc.wantOldToNew)
	}
	if rep.NewToOld != tc.wantNewToOld {
		t.Errorf("audit new->old = %s, want %s", rep.NewToOld, tc.wantNewToOld)
	}

	// Regenerate fresh packages for codegen so the audit cannot have touched
	// generation state (Compare is read-only anyway, but keep the inputs
	// independent for realism).
	oldGen := mustParseForCross(t, filepath.Join("testdata", "compat", tc.oldFile))
	newGen := mustParseForCross(t, filepath.Join("testdata", "compat", tc.newFile))
	setSizeMax(oldGen, tc.oldSizeMax)
	setSizeMax(newGen, tc.newSizeMax)

	if err := GenerateGo(oldDir, oldGen); err != nil {
		t.Fatalf("generate old: %s", err)
	}
	if err := GenerateGo(newDir, newGen); err != nil {
		t.Fatalf("generate new: %s", err)
	}

	// Locate produced Colfer.go files (package dir is based on pkg name).
	oldCode := findColferGo(t, oldDir)
	newCode := findColferGo(t, newDir)

	// Lay out a throwaway module that imports both revisions.
	modDir := filepath.Join(root, "mod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	placeRevision(t, modDir, "oldrev", oldCode)
	placeRevision(t, modDir, "newrev", newCode)
	writeCrossModule(t, modDir)
	writeCrossDriver(t, modDir, tc)

	gocache := filepath.Join(root, "gocache")
	cmd := exec.Command("go", "test", "-run", "TestCross", "-v", ".")
	cmd.Dir = modDir
	cmd.Env = append(os.Environ(),
		"GO111MODULE=on",
		"GOCACHE="+gocache,
		"GOPATH="+filepath.Join(root, "gopath"),
		"GOPROXY=off",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cross driver failed:\n%s", string(out))
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("cross driver did not report PASS:\n%s", string(out))
	}
}

func mustParseForCross(t *testing.T, path string) Packages {
	t.Helper()
	pkgs, err := ParseFiles(path)
	if err != nil {
		t.Fatalf("parse %s: %s", path, err)
	}
	return pkgs
}

func setSizeMax(pkgs Packages, expr string) {
	for _, p := range pkgs {
		p.SizeMax = expr
		p.ListMax = "64 * 1024"
	}
}

func findColferGo(t *testing.T, root string) string {
	t.Helper()
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name() == "Colfer.go" {
			found = path
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == "" {
		t.Fatalf("no Colfer.go under %s", root)
	}
	return found
}

func placeRevision(t *testing.T, modDir, revName, codePath string) {
	t.Helper()
	dstDir := filepath.Join(modDir, revName, "gen")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(codePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dstDir, "Colfer.go"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCrossModule(t *testing.T, modDir string) {
	t.Helper()
	content := "module crossverify\n\ngo 1.18\n"
	if err := os.WriteFile(filepath.Join(modDir, "go.mod"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCrossDriver(t *testing.T, modDir string, tc crossCase) {
	t.Helper()
	var b strings.Builder
	b.WriteString("package main\n\n")

	usesOld := len(tc.newToOld) != 0
	usesNew := len(tc.oldToNew) != 0

	b.WriteString("import (\n")
	b.WriteString("\t\"encoding/hex\"\n")
	if len(tc.oldToNew)+len(tc.newToOld) > 0 {
		for _, ex := range append(append([]crossExpect(nil), tc.oldToNew...), tc.newToOld...) {
			if !ex.ok && ex.errSubstr != "" {
				b.WriteString("\t\"strings\"\n")
				break
			}
		}
	}
	b.WriteString("\t\"testing\"\n")
	if usesOld {
		b.WriteString("\told \"crossverify/oldrev/gen\"\n")
	}
	if usesNew {
		b.WriteString("\tnew \"crossverify/newrev/gen\"\n")
	}
	b.WriteString(")\n\n")

	// Build a message value that the writer marshals to the fixed bytes.
	// Rather than construct typed values, the driver feeds fixed bytes to the
	// opposite decoder directly, which is exactly the cross-read contract.
	b.WriteString("func TestCross(t *testing.T) {\n")
	writeExpectLoop(&b, "OldToNew", tc.oldToNew, "new", tc.typ)
	writeExpectLoop(&b, "NewToOld", tc.newToOld, "old", tc.typ)
	b.WriteString("}\n")

	if err := os.WriteFile(filepath.Join(modDir, "cross_test.go"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExpectLoop(b *strings.Builder, label string, expects []crossExpect, readerSide, typ string) {
	if len(expects) == 0 {
		return
	}
	b.WriteString("\t// " + label + "\n")
	for _, ex := range expects {
		if _, err := hex.DecodeString(ex.serialHex); err != nil {
			panic("bad fixture hex: " + ex.serialHex)
		}
		b.WriteString("\t{\n")
		b.WriteString("\t\tdata, _ := hex.DecodeString(\"" + ex.serialHex + "\")\n")
		switch readerSide {
		case "new":
			b.WriteString("\t\tvar v new." + typ + "\n")
		case "old":
			b.WriteString("\t\tvar v old." + typ + "\n")
		}
		b.WriteString("\t\terr := v.UnmarshalBinary(data)\n")
		if ex.ok {
			b.WriteString("\t\tif err != nil { t.Fatalf(\"" + label + " expected accept, got %v\", err) }\n")
		} else {
			b.WriteString("\t\tif err == nil { t.Fatalf(\"" + label + " expected rejection\") }\n")
			b.WriteString("\t\tif !strings.Contains(err.Error(), \"" + ex.errSubstr + "\") {\n")
			b.WriteString("\t\t\tt.Fatalf(\"" + label + " error %q does not mention " + ex.errSubstr + "\", err.Error())\n")
			b.WriteString("\t\t}\n")
		}
		b.WriteString("\t}\n")
	}
}
