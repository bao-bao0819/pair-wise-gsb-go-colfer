package colfer

// Compatibility auditing between two schema revisions, based on the Colfer
// wire format and the behaviour of the generated decoders rather than on a
// textual diff. Fields are addressed by their declaration index because the
// wire only carries the index in the header byte: names are not transmitted.

import (
	"fmt"
	"sort"
	"strings"
)

// Verdict classifies a compatibility direction.
type Verdict int

const (
	// VerdictCompatible means every serial that the writer can produce
	// decodes without loss under the reader's schema.
	VerdictCompatible Verdict = iota
	// VerdictIncompatible means the writer can produce at least one serial
	// which the reader's generated decoder rejects or misinterprets.
	VerdictIncompatible
	// VerdictUnknown means no incompatibility could be proven, yet full
	// compatibility could not be confirmed either.
	VerdictUnknown
)

// String returns the canonical label.
func (v Verdict) String() string {
	switch v {
	case VerdictCompatible:
		return "compatible"
	case VerdictIncompatible:
		return "incompatible"
	default:
		return "unknown"
	}
}

// Limits are the generated upper bounds for a revision. Empty values disable
// the corresponding check.
type Limits struct {
	SizeMax string
	ListMax string
}

// Code identifies a specific wire-format rule that a finding documents.
type Code string

const (
	// CodeRename: field renamed only; the header index keeps the same encoding.
	CodeRename Code = "field-renamed"
	// CodeReorder: a field moved to another index while keeping its shape;
	// cross-read attributes the value to a different field.
	CodeReorder Code = "field-reordered"
	// CodeTailAppend: a new field added after every existing field.
	CodeTailAppend Code = "field-added-tail"
	// CodeInsert: a new field appeared at an index that an existing field
	// already occupies in the other revision, shifting later indices.
	CodeInsert Code = "field-inserted"
	// CodeTailRemove: a trailing field disappeared.
	CodeTailRemove Code = "field-removed-tail"
	// CodeRemove: a non-trailing field disappeared, shifting later indices.
	CodeRemove Code = "field-removed"
	// CodeScalarType: scalar (or scalar list) element encoding changed.
	CodeScalarType Code = "scalar-type-changed"
	// CodeScalarListFlip: a scalar became a list or vice versa.
	CodeScalarListFlip Code = "scalar-list-flip"
	// CodeNestedType: a nested struct reference points at a different type
	// declaration on the two sides.
	CodeNestedType Code = "nested-type-changed"
	// CodeNestedListFlip: a single nested struct became a list or vice versa.
	CodeNestedListFlip Code = "nested-list-flip"
	// CodeNestedEvolve: a referenced struct is itself incompatible and the
	// incompatibility propagates through this field.
	CodeNestedEvolve Code = "nested-schema-changed"
	// CodeSizeMaxTightened: the reader's size bound rejects writer serials.
	CodeSizeMaxTightened Code = "size-max-tightened"
	// CodeSizeMaxUnknown: the size bounds cannot be compared statically.
	CodeSizeMaxUnknown Code = "size-max-unknown"
	// CodeListMaxTightened: the reader's element-count bound rejects serials.
	CodeListMaxTightened Code = "list-max-tightened"
	// CodeListMaxUnknown: the element-count bounds cannot be compared.
	CodeListMaxUnknown Code = "list-max-unknown"
	// CodeStructAdded: the struct exists only in the newer revision.
	CodeStructAdded Code = "struct-added"
	// CodeStructRemoved: the struct existed only in the older revision.
	CodeStructRemoved Code = "struct-removed"
)

// PathStep records one hop towards the field that breaks a wire assumption.
type PathStep struct {
	// Struct is the qualified struct name (pkg.Type) where the field lives.
	Struct string
	// Field is the name on the traversed side.
	Field string
	// Index is the wire header index of Field.
	Index int
}

func (s PathStep) String() string {
	return fmt.Sprintf("%s.%s(index %d)", s.Struct, s.Field, s.Index)
}

// Finding documents a single rule evaluation. OldToNew and NewToOld carry the
// verdict for each reading direction.
type Finding struct {
	Code     Code       `json:"code"`
	Struct   string     `json:"struct,omitempty"`
	OldIndex int        `json:"old_index,omitempty"`
	NewIndex int        `json:"new_index,omitempty"`
	OldField string     `json:"old_field,omitempty"`
	NewField string     `json:"new_field,omitempty"`
	OldType  string     `json:"old_type,omitempty"`
	NewType  string     `json:"new_type,omitempty"`
	Path     []PathStep `json:"path,omitempty"`
	Detail   string     `json:"detail"`
	OldToNew Verdict    `json:"old_write_new_read"`
	NewToOld Verdict    `json:"new_write_old_read"`
}

// StructReport groups findings for one qualified struct and holds the combined
// verdict per direction.
type StructReport struct {
	QName    string    `json:"struct"`
	OldToNew Verdict   `json:"old_write_new_read"`
	NewToOld Verdict   `json:"new_write_old_read"`
	Findings []Finding `json:"findings"`
}

// PackageReport covers package-level evaluations (bounds and struct sets).
type PackageReport struct {
	OldName  string          `json:"old_package"`
	NewName  string          `json:"new_package"`
	OldToNew Verdict         `json:"old_write_new_read"`
	NewToOld Verdict         `json:"new_write_old_read"`
	Findings []Finding       `json:"findings"`
	Structs  []*StructReport `json:"structs"`
}

// Report is the complete comparison result.
type Report struct {
	OldToNew Verdict          `json:"old_write_new_read"`
	NewToOld Verdict          `json:"new_write_old_read"`
	Packages []*PackageReport `json:"packages"`
}

// compatContext holds the per-comparison state.
type compatContext struct {
	oldByQ map[string]*Struct
	newByQ map[string]*Struct

	nodes map[string]*compatNode

	pkgOld map[string]*Package
	pkgNew map[string]*Package

	oldBounds map[string]Limits
	newBounds map[string]Limits
}

// compatNode records direct findings and outgoing struct-reference edges.
type compatNode struct {
	q          string
	oldStruct  *Struct
	newStruct  *Struct
	direct     []Finding
	propagated []Finding
	edges      []compatEdge
	oldToNew   Verdict
	newToOld   Verdict
}

// compatEdge is a hop from a struct to a referenced struct through a field.
type compatEdge struct {
	to               *compatNode
	stepOld, stepNew PathStep
}

// Compare evaluates wire-format compatibility of two schema revisions.
//
// oldPkgs and newPkgs are the two parsed revisions. oldBounds and newBounds
// assign the generated ColferSizeMax/ColferListMax expressions per package
// name; pass nil maps (or leave a package unset) to skip bound checks.
//
// The inputs are read only: generation state such as NameNative and TypeNative
// is never written.
func Compare(oldPkgs, newPkgs Packages, oldBounds, newBounds map[string]Limits) *Report {
	ctx := &compatContext{
		oldByQ:    oldPkgs.StructsByQName(),
		newByQ:    newPkgs.StructsByQName(),
		nodes:     make(map[string]*compatNode),
		pkgOld:    make(map[string]*Package),
		pkgNew:    make(map[string]*Package),
		oldBounds: oldBounds,
		newBounds: newBounds,
	}
	for _, p := range oldPkgs {
		ctx.pkgOld[p.Name] = p
	}
	for _, p := range newPkgs {
		ctx.pkgNew[p.Name] = p
	}

	for q, oldT := range ctx.oldByQ {
		if newT, ok := ctx.newByQ[q]; ok {
			ctx.nodes[q] = &compatNode{q: q, oldStruct: oldT, newStruct: newT}
		}
	}

	pkgNames := make(map[string]struct{})
	for n := range ctx.pkgOld {
		pkgNames[n] = struct{}{}
	}
	for n := range ctx.pkgNew {
		pkgNames[n] = struct{}{}
	}
	var names []string
	for n := range pkgNames {
		names = append(names, n)
	}
	sort.Strings(names)

	report := &Report{}
	pkgReports := map[string]*PackageReport{}
	for _, n := range names {
		pr := &PackageReport{OldName: n, NewName: n}
		pkgReports[n] = pr
		report.Packages = append(report.Packages, pr)

		oldP := ctx.pkgOld[n]
		newP := ctx.pkgNew[n]
		ctx.evalPackageBounds(n, oldP, newP, pr)

		var added []string
		if newP != nil {
			for _, t := range newP.Structs {
				if _, ok := ctx.oldByQ[t.String()]; !ok {
					added = append(added, t.String())
				}
			}
		}
		sort.Strings(added)
		for _, q := range added {
			pr.Findings = append(pr.Findings, Finding{
				Code:   CodeStructAdded,
				Struct: q,
				Detail: fmt.Sprintf(
					"struct %s exists only in the new schema; the wire has no type tag, so an old decoder can only reach it through an old field that already references a compatible slot",
					q),
				OldToNew: VerdictCompatible,
				NewToOld: VerdictCompatible,
			})
		}

		var removed []string
		if oldP != nil {
			for _, t := range oldP.Structs {
				if _, ok := ctx.newByQ[t.String()]; !ok {
					removed = append(removed, t.String())
				}
			}
		}
		sort.Strings(removed)
		for _, q := range removed {
			pr.Findings = append(pr.Findings, Finding{
				Code:   CodeStructRemoved,
				Struct: q,
				Detail: fmt.Sprintf(
					"struct %s exists only in the old schema; serials referencing it have no counterpart in the new schema",
					q),
				OldToNew: VerdictCompatible,
				NewToOld: VerdictCompatible,
			})
		}
	}

	for _, node := range ctx.nodes {
		ctx.analyzeNode(node)
	}

	ctx.propagate()

	for _, node := range ctx.nodes {
		ctx.attachPropagated(node)
	}

	for _, n := range names {
		pr := pkgReports[n]
		pr.OldToNew = findingsVerdict(pr.Findings, true)
		pr.NewToOld = findingsVerdict(pr.Findings, false)

		var qnames []string
		for _, node := range ctx.nodes {
			if pkgOfQName(node.q) == n {
				qnames = append(qnames, node.q)
			}
		}
		sort.Strings(qnames)
		for _, q := range qnames {
			node := ctx.nodes[q]
			findings := append([]Finding(nil), node.direct...)
			findings = append(findings, node.propagated...)
			sr := &StructReport{
				QName:    q,
				OldToNew: node.oldToNew,
				NewToOld: node.newToOld,
				Findings: findings,
			}
			sortStructFindings(sr.Findings)
			pr.Structs = append(pr.Structs, sr)

			pr.OldToNew = combineVerdict([]Verdict{pr.OldToNew, sr.OldToNew})
			pr.NewToOld = combineVerdict([]Verdict{pr.NewToOld, sr.NewToOld})
		}
		report.OldToNew = combineVerdict([]Verdict{report.OldToNew, pr.OldToNew})
		report.NewToOld = combineVerdict([]Verdict{report.NewToOld, pr.NewToOld})
	}

	return report
}

func pkgOfQName(q string) string {
	if i := strings.LastIndexByte(q, '.'); i >= 0 {
		return q[:i]
	}
	return q
}

func findingsVerdict(fs []Finding, oldToNew bool) Verdict {
	vs := make([]Verdict, 0, len(fs))
	for i := range fs {
		if oldToNew {
			vs = append(vs, fs[i].OldToNew)
		} else {
			vs = append(vs, fs[i].NewToOld)
		}
	}
	return combineVerdict(vs)
}

// combineVerdict yields the worst verdict: incompatible dominates unknown,
// unknown dominates compatible.
func combineVerdict(vs []Verdict) Verdict {
	best := VerdictCompatible
	for _, v := range vs {
		if v == VerdictIncompatible {
			return VerdictIncompatible
		}
		if v == VerdictUnknown {
			best = VerdictUnknown
		}
	}
	return best
}

func sortStructFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := findingSortKey(&fs[i]), findingSortKey(&fs[j])
		if a.index != b.index {
			return a.index < b.index
		}
		if a.code != b.code {
			return a.code < b.code
		}
		if a.oldTyp != b.oldTyp {
			return a.oldTyp < b.oldTyp
		}
		if a.newTyp != b.newTyp {
			return a.newTyp < b.newTyp
		}
		return a.detail < b.detail
	})
}

type findingSort struct {
	index  int
	code   Code
	oldTyp string
	newTyp string
	detail string
}

func findingSortKey(f *Finding) findingSort {
	idx := f.OldIndex
	if idx == 0 && f.NewIndex != 0 {
		idx = f.NewIndex
	}
	if len(f.Path) != 0 && f.Code == CodeNestedEvolve {
		idx = f.Path[0].Index
	}
	return findingSort{idx, f.Code, f.OldType, f.NewType, f.Detail}
}
