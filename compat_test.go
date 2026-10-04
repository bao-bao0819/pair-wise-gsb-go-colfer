package colfer

import (
	"bytes"
	"sort"
	"testing"
)

func sortStructsForTest(p *Package) {
	sort.Slice(p.Structs, func(i, j int) bool {
		return p.Structs[i].Name < p.Structs[j].Name
	})
}

// compatFixture builds a small two-struct package in memory.
func compatFixture(t *testing.T, defs map[string][][2]string) (Packages, map[string]*Struct) {
	t.Helper()
	p := &Package{Name: "audit"}
	byName := make(map[string]*Struct)
	for name, fields := range defs {
		_ = fields
		s := &Struct{Pkg: p, Name: name}
		p.Structs = append(p.Structs, s)
		byName[name] = s
	}
	// Deterministic struct order for the fixture itself.
	sortStructsForTest(p)
	for name, fields := range defs {
		s := byName[name]
		for i, f := range fields {
			typ := f[0]
			field := &Field{Struct: s, Index: i, Name: f[1]}
			decorateField(field, typ, byName)
			s.Fields = append(s.Fields, field)
		}
	}
	return Packages{p}, byName
}

func decorateField(field *Field, typ string, byName map[string]*Struct) {
	for len(typ) >= 2 && typ[:2] == "[]" {
		field.TypeList = true
		typ = typ[2:]
	}
	if s, ok := byName[typ]; ok {
		field.TypeRef = s
		field.Type = "x"
	} else {
		field.Type = typ
	}
}

func reportForStruct(t *testing.T, rep *Report, qname string) *StructReport {
	t.Helper()
	for _, pr := range rep.Packages {
		for _, sr := range pr.Structs {
			if sr.QName == qname {
				return sr
			}
		}
	}
	t.Fatalf("struct report %s not found", qname)
	return nil
}

func codes(sr *StructReport) []Code {
	var out []Code
	for _, f := range sr.Findings {
		out = append(out, f.Code)
	}
	return out
}

func findingByCode(sr *StructReport, c Code) *Finding {
	for i := range sr.Findings {
		if sr.Findings[i].Code == c {
			return &sr.Findings[i]
		}
	}
	return nil
}

func TestCompareTailAppendAndRemove(t *testing.T) {
	old, _ := compatFixture(t, map[string][][2]string{
		"Rec": {{"uint32", "a"}},
	})
	neu, _ := compatFixture(t, map[string][][2]string{
		"Rec": {{"uint32", "a"}, {"uint32", "b"}},
	})
	rep := Compare(old, neu, nil, nil)
	sr := reportForStruct(t, rep, "audit.Rec")
	if got, want := sr.OldToNew, VerdictCompatible; got != want {
		t.Errorf("append old->new = %s, want %s", got, want)
	}
	if got, want := sr.NewToOld, VerdictIncompatible; got != want {
		t.Errorf("append new->old = %s, want %s", got, want)
	}
	if f := findingByCode(sr, CodeTailAppend); f == nil {
		t.Errorf("want field-added-tail finding, got %v", codes(sr))
	}

	// Reverse direction: tail removal.
	rep2 := Compare(neu, old, nil, nil)
	sr2 := reportForStruct(t, rep2, "audit.Rec")
	if sr2.OldToNew != VerdictIncompatible || sr2.NewToOld != VerdictCompatible {
		t.Errorf("remove verdicts = %s/%s, want incompatible/compatible", sr2.OldToNew, sr2.NewToOld)
	}
	if findingByCode(sr2, CodeTailRemove) == nil {
		t.Errorf("want field-removed-tail finding, got %v", codes(sr2))
	}
}

func TestCompareRename(t *testing.T) {
	old, _ := compatFixture(t, map[string][][2]string{
		"Rec": {{"uint32", "a"}},
	})
	neu, _ := compatFixture(t, map[string][][2]string{
		"Rec": {{"uint32", "renamed"}},
	})
	rep := Compare(old, neu, nil, nil)
	sr := reportForStruct(t, rep, "audit.Rec")
	if sr.OldToNew != VerdictCompatible || sr.NewToOld != VerdictCompatible {
		t.Errorf("rename verdicts = %s/%s, want compatible/compatible", sr.OldToNew, sr.NewToOld)
	}
	if findingByCode(sr, CodeRename) == nil {
		t.Errorf("want field-renamed finding, got %v", codes(sr))
	}
}

func TestCompareReorder(t *testing.T) {
	old, _ := compatFixture(t, map[string][][2]string{
		"Rec": {{"uint32", "a"}, {"uint64", "b"}},
	})
	neu, _ := compatFixture(t, map[string][][2]string{
		"Rec": {{"uint64", "b"}, {"uint32", "a"}},
	})
	rep := Compare(old, neu, nil, nil)
	sr := reportForStruct(t, rep, "audit.Rec")
	if sr.OldToNew != VerdictIncompatible || sr.NewToOld != VerdictIncompatible {
		t.Errorf("reorder verdicts = %s/%s", sr.OldToNew, sr.NewToOld)
	}
	f := findingByCode(sr, CodeReorder)
	if f == nil {
		t.Fatalf("want reorder finding, got %v", codes(sr))
	}
	if !(f.OldIndex == 1 || f.NewIndex == 1) {
		t.Errorf("reorder indices = %d/%d", f.OldIndex, f.NewIndex)
	}
}

func TestCompareScalarTypeAndListFlip(t *testing.T) {
	old, _ := compatFixture(t, map[string][][2]string{
		"Rec": {{"uint32", "a"}, {"float64", "b"}},
	})
	neu, _ := compatFixture(t, map[string][][2]string{
		"Rec": {{"int32", "a"}, {"[]float64", "b"}},
	})
	rep := Compare(old, neu, nil, nil)
	sr := reportForStruct(t, rep, "audit.Rec")
	if sr.OldToNew != VerdictIncompatible || sr.NewToOld != VerdictIncompatible {
		t.Errorf("type-change verdicts = %s/%s", sr.OldToNew, sr.NewToOld)
	}
	if findingByCode(sr, CodeScalarType) == nil {
		t.Errorf("want scalar-type-changed, got %v", codes(sr))
	}
	if findingByCode(sr, CodeScalarListFlip) == nil {
		t.Errorf("want scalar-list-flip, got %v", codes(sr))
	}
}

func TestCompareTextBinaryCompatible(t *testing.T) {
	old, _ := compatFixture(t, map[string][][2]string{
		"Rec": {{"text", "a"}, {"[]binary", "b"}},
	})
	neu, _ := compatFixture(t, map[string][][2]string{
		"Rec": {{"binary", "a"}, {"[]text", "b"}},
	})
	rep := Compare(old, neu, nil, nil)
	sr := reportForStruct(t, rep, "audit.Rec")
	if sr.OldToNew != VerdictCompatible || sr.NewToOld != VerdictCompatible {
		t.Errorf("text<->binary verdicts = %s/%s", sr.OldToNew, sr.NewToOld)
	}
}

func TestCompareInteriorInsert(t *testing.T) {
	old, _ := compatFixture(t, map[string][][2]string{
		"Rec": {{"uint32", "a"}, {"uint32", "c"}},
	})
	neu, _ := compatFixture(t, map[string][][2]string{
		"Rec": {{"uint32", "a"}, {"uint32", "b"}, {"uint32", "c"}},
	})
	rep := Compare(old, neu, nil, nil)
	sr := reportForStruct(t, rep, "audit.Rec")
	if sr.OldToNew != VerdictIncompatible || sr.NewToOld != VerdictIncompatible {
		t.Errorf("interior insert verdicts = %s/%s", sr.OldToNew, sr.NewToOld)
	}
	if findingByCode(sr, CodeInsert) == nil {
		t.Errorf("want field-inserted finding, got %v", codes(sr))
	}
}

func TestCompareInteriorRemove(t *testing.T) {
	old, _ := compatFixture(t, map[string][][2]string{
		"Rec": {{"uint32", "a"}, {"uint32", "b"}, {"uint32", "c"}},
	})
	neu, _ := compatFixture(t, map[string][][2]string{
		"Rec": {{"uint32", "a"}, {"uint32", "c"}},
	})
	rep := Compare(old, neu, nil, nil)
	sr := reportForStruct(t, rep, "audit.Rec")
	if sr.OldToNew != VerdictIncompatible || sr.NewToOld != VerdictIncompatible {
		t.Errorf("interior remove verdicts = %s/%s", sr.OldToNew, sr.NewToOld)
	}
	if findingByCode(sr, CodeInsert) == nil && findingByCode(sr, CodeRemove) == nil {
		t.Errorf("want interior insert/remove finding, got %v", codes(sr))
	}
}

func TestCompareSameIndexDifferentShapeIsNotRename(t *testing.T) {
	old, _ := compatFixture(t, map[string][][2]string{
		"Rec": {{"uint32", "a"}, {"uint64", "b"}},
	})
	neu, _ := compatFixture(t, map[string][][2]string{
		"Rec": {{"uint32", "a"}, {"text", "c"}},
	})
	rep := Compare(old, neu, nil, nil)
	sr := reportForStruct(t, rep, "audit.Rec")
	if findingByCode(sr, CodeRename) != nil {
		t.Errorf("unexpected rename finding: %v", codes(sr))
	}
	// Same index, incompatible shapes: b is removed and c inserted at index 1.
	if sr.OldToNew != VerdictIncompatible || sr.NewToOld != VerdictIncompatible {
		t.Errorf("verdicts = %s/%s, want incompatible/incompatible", sr.OldToNew, sr.NewToOld)
	}
}

func TestCompareNestedTypeAndPropagation(t *testing.T) {
	old, oldByName := compatFixture(t, map[string][][2]string{
		"Inner": {{"uint32", "x"}},
		"Outer": {{"Inner", "in"}},
	})
	neu, newByName := compatFixture(t, map[string][][2]string{
		"Inner": {{"uint64", "x"}},
		"Outer": {{"Inner", "in"}},
	})
	// Ensure refs point at the right side structs (already set by fixture).
	_ = oldByName
	_ = newByName

	rep := Compare(old, neu, nil, nil)
	inner := reportForStruct(t, rep, "audit.Inner")
	if findingByCode(inner, CodeScalarType) == nil {
		t.Errorf("inner scalar change missing, got %v", codes(inner))
	}
	outer := reportForStruct(t, rep, "audit.Outer")
	if outer.OldToNew != VerdictIncompatible || outer.NewToOld != VerdictIncompatible {
		t.Errorf("outer propagation verdicts = %s/%s", outer.OldToNew, outer.NewToOld)
	}
	pf := findingByCode(outer, CodeNestedEvolve)
	if pf == nil {
		t.Fatalf("want propagated finding, got %v", codes(outer))
	}
	if len(pf.Path) != 1 || pf.Path[0].Field != "in" || pf.Path[0].Index != 0 {
		t.Errorf("unexpected propagation path: %+v", pf.Path)
	}
}

func TestCompareNestedListFlip(t *testing.T) {
	old, _ := compatFixture(t, map[string][][2]string{
		"Inner": {{"uint32", "x"}},
		"Outer": {{"Inner", "in"}},
	})
	neu, _ := compatFixture(t, map[string][][2]string{
		"Inner": {{"uint32", "x"}},
		"Outer": {{"[]Inner", "in"}},
	})
	rep := Compare(old, neu, nil, nil)
	outer := reportForStruct(t, rep, "audit.Outer")
	if findingByCode(outer, CodeNestedListFlip) == nil {
		t.Errorf("want nested-list-flip, got %v", codes(outer))
	}
}

func TestCompareDeepPropagationPath(t *testing.T) {
	old, _ := compatFixture(t, map[string][][2]string{
		"Leaf": {{"uint32", "v"}},
		"Mid":  {{"Leaf", "leaf"}},
		"Root": {{"Mid", "mid"}},
	})
	neu, _ := compatFixture(t, map[string][][2]string{
		"Leaf": {{"uint64", "v"}},
		"Mid":  {{"Leaf", "leaf"}},
		"Root": {{"Mid", "mid"}},
	})
	rep := Compare(old, neu, nil, nil)

	mid := reportForStruct(t, rep, "audit.Mid")
	mf := findingByCode(mid, CodeNestedEvolve)
	if mf == nil || len(mf.Path) != 1 || mf.Path[0].Field != "leaf" {
		t.Fatalf("mid propagation path wrong: %+v", mf)
	}

	root := reportForStruct(t, rep, "audit.Root")
	rf := findingByCode(root, CodeNestedEvolve)
	if rf == nil {
		t.Fatalf("root missing propagated finding: %v", codes(root))
	}
	if len(rf.Path) != 2 ||
		rf.Path[0].Field != "mid" || rf.Path[0].Struct != "audit.Root" ||
		rf.Path[1].Field != "leaf" || rf.Path[1].Struct != "audit.Mid" {
		t.Fatalf("root propagation path wrong: %+v", rf.Path)
	}
}

func TestCompareRecursiveTerminates(t *testing.T) {
	old, _ := compatFixture(t, map[string][][2]string{
		"Node": {{"uint32", "val"}, {"Node", "next"}},
	})
	neu, _ := compatFixture(t, map[string][][2]string{
		"Node": {{"uint64", "val"}, {"Node", "next"}},
	})
	rep := Compare(old, neu, nil, nil)
	sr := reportForStruct(t, rep, "audit.Node")
	if sr.OldToNew != VerdictIncompatible || sr.NewToOld != VerdictIncompatible {
		t.Errorf("recursive verdicts = %s/%s", sr.OldToNew, sr.NewToOld)
	}
	if findingByCode(sr, CodeScalarType) == nil {
		t.Errorf("want direct scalar finding on recursive type, got %v", codes(sr))
	}
}

func TestCompareSizeMaxTightened(t *testing.T) {
	old, _ := compatFixture(t, map[string][][2]string{"Rec": {{"uint32", "a"}}})
	neu, _ := compatFixture(t, map[string][][2]string{"Rec": {{"uint32", "a"}}})

	// New reader is stricter.
	rep := Compare(old, neu,
		map[string]Limits{"audit": {SizeMax: "1024"}},
		map[string]Limits{"audit": {SizeMax: "512"}})
	var got *Finding
	for _, pr := range rep.Packages {
		for i := range pr.Findings {
			if pr.Findings[i].Code == CodeSizeMaxTightened {
				got = &pr.Findings[i]
			}
		}
	}
	if got == nil {
		t.Fatalf("want size-max-tightened finding")
	}
	if got.OldToNew != VerdictIncompatible || got.NewToOld != VerdictCompatible {
		t.Errorf("tightened verdicts = %s/%s", got.OldToNew, got.NewToOld)
	}

	// Unevaluable expressions must be unknown, not a guessed verdict.
	rep2 := Compare(old, neu,
		map[string]Limits{"audit": {SizeMax: "MyLimit"}},
		map[string]Limits{"audit": {SizeMax: "1024"}})
	var unk bool
	for _, pr := range rep2.Packages {
		for _, f := range pr.Findings {
			if f.Code == CodeSizeMaxUnknown {
				unk = true
				if f.OldToNew != VerdictUnknown || f.NewToOld != VerdictUnknown {
					t.Errorf("unknown verdicts = %s/%s", f.OldToNew, f.NewToOld)
				}
			}
		}
	}
	if !unk {
		t.Errorf("want size-max-unknown finding")
	}
}

func TestCompareDoesNotMutateGenerationState(t *testing.T) {
	old, _ := compatFixture(t, map[string][][2]string{"Rec": {{"uint32", "a"}}})
	neu, _ := compatFixture(t, map[string][][2]string{"Rec": {{"uint32", "a"}, {"text", "b"}}})

	old[0].NameNative = "KeepOld"
	old[0].Structs[0].NameNative = "KeepType"
	old[0].Structs[0].Fields[0].NameNative = "KeepField"
	old[0].Structs[0].Fields[0].TypeNative = "KeepTypeNative"
	neu[0].NameNative = "KeepNew"

	_ = Compare(old, neu, nil, nil)

	if old[0].NameNative != "KeepOld" ||
		old[0].Structs[0].NameNative != "KeepType" ||
		old[0].Structs[0].Fields[0].NameNative != "KeepField" ||
		old[0].Structs[0].Fields[0].TypeNative != "KeepTypeNative" ||
		neu[0].NameNative != "KeepNew" {
		t.Errorf("comparison mutated generation state")
	}
}

func TestCompareReportStable(t *testing.T) {
	old, _ := compatFixture(t, map[string][][2]string{"Rec": {{"uint32", "a"}, {"uint64", "b"}}})
	neu, _ := compatFixture(t, map[string][][2]string{"Rec": {{"int32", "a"}, {"[]uint64", "b"}}})

	var first, second bytes.Buffer
	r1 := Compare(old, neu, nil, nil)
	r2 := Compare(old, neu, nil, nil)
	_ = r1.WriteText(&first)
	_ = r2.WriteText(&second)
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Errorf("report rendering is not deterministic")
	}
}
