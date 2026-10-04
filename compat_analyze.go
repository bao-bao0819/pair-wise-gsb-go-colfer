package colfer

import (
	"fmt"
	"strings"
)

// analyzeNode compares the two field lists of one struct, records direct
// findings and registers edges towards referenced structs.
func (ctx *compatContext) analyzeNode(node *compatNode) {
	oldFields := node.oldStruct.Fields
	newFields := node.newStruct.Fields

	// Longest common subsequence on names keeps the pairing order-stable.
	pairedOld := make(map[int]bool)
	pairedNew := make(map[int]bool)
	for _, pair := range lcsFieldPairs(oldFields, newFields) {
		of, nf := oldFields[pair.old], newFields[pair.new]
		pairedOld[pair.old] = true
		pairedNew[pair.new] = true
		ctx.comparePaired(node, of, nf)
	}

	var unmatchedOld []*Field
	var unmatchedNew []*Field
	for i, f := range oldFields {
		if !pairedOld[i] {
			unmatchedOld = append(unmatchedOld, f)
		}
	}
	for i, f := range newFields {
		if !pairedNew[i] {
			unmatchedNew = append(unmatchedNew, f)
		}
	}

	usedOld := make(map[int]bool)
	usedNew := make(map[int]bool)
	// Match renames. A rename cannot move bytes on the wire into a slot that
	// the other revision reserves for another unmatched field, so only fields
	// that share an index are eligible; this also keeps a removed-tail field
	// from being "renamed" into an unrelated appended field.
	for ni, nf := range unmatchedNew {
		for oi, of := range unmatchedOld {
			if usedOld[oi] || of.Index != nf.Index {
				continue
			}
			if !sameWireShape(of, nf) {
				continue
			}
			usedOld[oi] = true
			usedNew[ni] = true
			ctx.compareRenamed(node, of, nf)
			break
		}
	}

	for oi, of := range unmatchedOld {
		if usedOld[oi] {
			continue
		}
		ctx.compareRemoved(node, of, len(newFields))
	}
	for ni, nf := range unmatchedNew {
		if usedNew[ni] {
			continue
		}
		ctx.compareAdded(node, nf, len(oldFields))
	}

	node.oldToNew = findingsVerdict(node.direct, true)
	node.newToOld = findingsVerdict(node.direct, false)
}

type indexPair struct{ old, new int }

// lcsFieldPairs returns index pairs for the longest common subsequence of
// field names, ascending on both sides.
func lcsFieldPairs(oldFields, newFields []*Field) []indexPair {
	m, n := len(oldFields), len(newFields)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if oldFields[i].Name == newFields[j].Name {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	var pairs []indexPair
	i, j := 0, 0
	for i < m && j < n {
		if oldFields[i].Name == newFields[j].Name {
			pairs = append(pairs, indexPair{i, j})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			i++
		} else {
			j++
		}
	}
	return pairs
}

// comparePaired handles two fields that share their name across revisions.
func (ctx *compatContext) comparePaired(node *compatNode, of, nf *Field) {
	if of.Index != nf.Index {
		node.direct = append(node.direct, reorderFinding(of, nf))
	}
	ctx.compareWire(node, of, nf)
}

func reorderFinding(of, nf *Field) Finding {
	return Finding{
		Code:     CodeReorder,
		Struct:   of.Struct.String(),
		OldIndex: of.Index,
		NewIndex: nf.Index,
		OldField: of.Name,
		NewField: nf.Name,
		OldType:  typeLabel(of),
		NewType:  typeLabel(nf),
		Detail: fmt.Sprintf(
			"field %q moved from header index %d to %d; the generated decoder dispatches each header byte to the field declared at that index and never skips headers, so the value is silently assigned to another field or desynchronises the stream",
			of.Name, of.Index, nf.Index),
		OldToNew: VerdictIncompatible,
		NewToOld: VerdictIncompatible,
	}
}

// compareRenamed handles two equally-shaped fields with different names.
func (ctx *compatContext) compareRenamed(node *compatNode, of, nf *Field) {
	f := Finding{
		Code:     CodeRename,
		Struct:   of.Struct.String(),
		OldIndex: of.Index,
		NewIndex: nf.Index,
		OldField: of.Name,
		NewField: nf.Name,
		OldType:  typeLabel(of),
		NewType:  typeLabel(nf),
		Detail: fmt.Sprintf(
			"field %q was renamed to %q at header index %d; the field name is not part of the wire format, so both directions decode the same bytes (only the language binding name changes)",
			of.Name, nf.Name, of.Index),
		OldToNew: VerdictCompatible,
		NewToOld: VerdictCompatible,
	}
	if of.Index != nf.Index {
		// A rename that also moves the slot behaves like a reorder on the wire.
		f.Code = CodeReorder
		f.Detail = fmt.Sprintf(
			"field %q (now %q) also moved from header index %d to %d; even though the encoding is unchanged, the index shift makes each decoder attribute the value to a different field",
			of.Name, nf.Name, of.Index, nf.Index)
		f.OldToNew = VerdictIncompatible
		f.NewToOld = VerdictIncompatible
	}
	node.direct = append(node.direct, f)

	// Same-shape nested structs may still carry propagated incompatibility.
	ctx.registerEdgeIfStruct(node, of, nf)
}

// compareAdded classifies a field that only the new revision declares.
func (ctx *compatContext) compareAdded(node *compatNode, nf *Field, oldCount int) {
	tail := nf.Index >= oldCount
	if tail {
		node.direct = append(node.direct, Finding{
			Code:     CodeTailAppend,
			Struct:   nf.Struct.String(),
			NewIndex: nf.Index,
			NewField: nf.Name,
			OldType:  "",
			NewType:  typeLabel(nf),
			Detail: fmt.Sprintf(
				"new field %q was appended at header index %d, after all %d old fields; an old writer never emits it and the new decoder leaves the zero value, but an old decoder that reads the header byte %d stops with \"unknown header\" because it has no field past index %d",
				nf.Name, nf.Index, oldCount, nf.Index, oldCount-1),
			// Old writes, new reads: fine. New writes, old reads: breaks when set.
			OldToNew: VerdictCompatible,
			NewToOld: VerdictIncompatible,
		})
		return
	}

	node.direct = append(node.direct, Finding{
		Code:     CodeInsert,
		Struct:   nf.Struct.String(),
		NewIndex: nf.Index,
		NewField: nf.Name,
		NewType:  typeLabel(nf),
		Detail: fmt.Sprintf(
			"new field %q was inserted at header index %d, shifting every later field; decoders do not skip unknown headers and fields after the insertion are read at the wrong index, so both directions desynchronise once the inserted or a shifted field is present",
			nf.Name, nf.Index),
		OldToNew: VerdictIncompatible,
		NewToOld: VerdictIncompatible,
	})
}

// compareRemoved classifies a field that only the old revision declares.
func (ctx *compatContext) compareRemoved(node *compatNode, of *Field, newCount int) {
	tail := of.Index >= newCount
	if tail {
		node.direct = append(node.direct, Finding{
			Code:     CodeTailRemove,
			Struct:   of.Struct.String(),
			OldIndex: of.Index,
			OldField: of.Name,
			OldType:  typeLabel(of),
			Detail: fmt.Sprintf(
				"old field %q at trailing header index %d was removed; a new writer never emits index %d and the old decoder leaves its zero value, but a new decoder that reads an old serial containing header %d stops with \"unknown header\"",
				of.Name, of.Index, of.Index, of.Index),
			OldToNew: VerdictIncompatible,
			NewToOld: VerdictCompatible,
		})
		return
	}

	node.direct = append(node.direct, Finding{
		Code:     CodeRemove,
		Struct:   of.Struct.String(),
		OldIndex: of.Index,
		OldField: of.Name,
		OldType:  typeLabel(of),
		Detail: fmt.Sprintf(
			"old field %q at interior header index %d was removed, shifting every later field; an old serial that sets it or any later field is dispatched to the wrong new field or ends with \"unknown header\"",
			of.Name, of.Index),
		OldToNew: VerdictIncompatible,
		NewToOld: VerdictIncompatible,
	})
}

// typeLabel renders a field type including list/nesting markers, without
// touching the generated TypeNative.
func typeLabel(f *Field) string {
	var b strings.Builder
	if f.TypeList {
		b.WriteString("[]")
	}
	if f.TypeRef != nil {
		b.WriteString(f.TypeRef.String())
	} else {
		b.WriteString(f.Type)
	}
	return b.String()
}
