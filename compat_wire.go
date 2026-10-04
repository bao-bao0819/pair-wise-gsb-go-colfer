package colfer

import "fmt"

// sameWireShape reports whether two fields use an identical on-the-wire shape:
// list flag, scalar kind, or referenced struct identity. text and binary are
// distinct schema types but identical on the wire, hence treated as equal.
func sameWireShape(a, b *Field) bool {
	if a.TypeList != b.TypeList {
		return false
	}
	if a.TypeRef != nil || b.TypeRef != nil {
		if a.TypeRef == nil || b.TypeRef == nil {
			return false
		}
		return a.TypeRef.String() == b.TypeRef.String()
	}
	return scalarKind(a.Type) == scalarKind(b.Type)
}

// scalarKind groups schema scalar types that share a wire encoding. text and
// binary both encode a varint length followed by raw bytes. All other types
// have their own encoding family.
func scalarKind(t string) string {
	switch t {
	case "text", "binary":
		return "bytes"
	default:
		return t
	}
}

// compareWire evaluates the encoding at a shared field position. It records
// scalar/list/nested findings and, when applicable, registers a propagation
// edge to the referenced struct.
func (ctx *compatContext) compareWire(node *compatNode, of, nf *Field) {
	oldStructField := of.TypeRef != nil
	newStructField := nf.TypeRef != nil

	switch {
	case oldStructField && newStructField:
		if of.TypeList != nf.TypeList {
			node.direct = append(node.direct, nestedListFlipFinding(of, nf))
			return
		}
		if of.TypeRef.String() != nf.TypeRef.String() {
			node.direct = append(node.direct, nestedTypeFinding(of, nf))
			return
		}
		ctx.registerEdgeIfStruct(node, of, nf)

	case oldStructField || newStructField:
		if of.TypeList != nf.TypeList {
			node.direct = append(node.direct, nestedListFlipFinding(of, nf))
		} else {
			node.direct = append(node.direct, nestedTypeFinding(of, nf))
		}

	default:
		ctx.compareScalar(node, of, nf)
	}
}

func (ctx *compatContext) compareScalar(node *compatNode, of, nf *Field) {
	if of.TypeList != nf.TypeList {
		node.direct = append(node.direct, scalarListFlipFinding(of, nf))
		return
	}
	if scalarKind(of.Type) == scalarKind(nf.Type) {
		if of.Type != nf.Type {
			// text <-> binary: schema-only distinction, same bytes.
			node.direct = append(node.direct, Finding{
				Code:     CodeScalarType,
				Struct:   of.Struct.String(),
				OldIndex: of.Index,
				NewIndex: nf.Index,
				OldField: of.Name,
				NewField: nf.Name,
				OldType:  typeLabel(of),
				NewType:  typeLabel(nf),
				Detail: fmt.Sprintf(
					"field %q changed %s to %s at index %d; both encode a varint length plus raw bytes with no header difference, so the serial is unchanged (UTF-8 validation is not performed by Go)",
					of.Name, typeLabel(of), typeLabel(nf), of.Index),
				OldToNew: VerdictCompatible,
				NewToOld: VerdictCompatible,
			})
		}
		return
	}

	node.direct = append(node.direct, scalarTypeFinding(of, nf))
}

func scalarListFlipFinding(of, nf *Field) Finding {
	return Finding{
		Code:     CodeScalarListFlip,
		Struct:   of.Struct.String(),
		OldIndex: of.Index,
		NewIndex: nf.Index,
		OldField: of.Name,
		NewField: nf.Name,
		OldType:  typeLabel(of),
		NewType:  typeLabel(nf),
		Detail: fmt.Sprintf(
			"field %q switched between scalar %s and list %s at index %d; a list serial starts with a varint element count and repeats the element encoding, while a scalar has no count, so the reader interprets the first value bytes as a length (or vice versa) and loses stream alignment",
			of.Name, typeLabel(of), typeLabel(nf), of.Index),
		OldToNew: VerdictIncompatible,
		NewToOld: VerdictIncompatible,
	}
}

func scalarTypeFinding(of, nf *Field) Finding {
	return Finding{
		Code:     CodeScalarType,
		Struct:   of.Struct.String(),
		OldIndex: of.Index,
		NewIndex: nf.Index,
		OldField: of.Name,
		NewField: nf.Name,
		OldType:  typeLabel(of),
		NewType:  typeLabel(nf),
		Detail: fmt.Sprintf(
			"field %q changed encoding at index %d: %s uses %s, while %s uses %s; the generated decoder reads a fixed/varint layout specific to its declared type, so crossing the two parses the same header payload as a different layout and either truncates reads or reports an unknown header",
			of.Name, of.Index, typeLabel(of), encodingDesc(of.Type, of.TypeList), typeLabel(nf), encodingDesc(nf.Type, nf.TypeList)),
		OldToNew: VerdictIncompatible,
		NewToOld: VerdictIncompatible,
	}
}

func nestedListFlipFinding(of, nf *Field) Finding {
	return Finding{
		Code:     CodeNestedListFlip,
		Struct:   of.Struct.String(),
		OldIndex: of.Index,
		NewIndex: nf.Index,
		OldField: of.Name,
		NewField: nf.Name,
		OldType:  typeLabel(of),
		NewType:  typeLabel(nf),
		Detail: fmt.Sprintf(
			"field %q switched between a single nested struct %s and a struct list %s at index %d; the list form is prefixed by a varint element count, whereas the single form embeds the inner 0x7f-terminated message directly, so the first byte is read as a count or as an inner header and alignment is lost",
			of.Name, typeLabel(of), typeLabel(nf), of.Index),
		OldToNew: VerdictIncompatible,
		NewToOld: VerdictIncompatible,
	}
}

func nestedTypeFinding(of, nf *Field) Finding {
	oldRef := typeLabel(of)
	newRef := typeLabel(nf)
	return Finding{
		Code:     CodeNestedType,
		Struct:   of.Struct.String(),
		OldIndex: of.Index,
		NewIndex: nf.Index,
		OldField: of.Name,
		NewField: nf.Name,
		OldType:  oldRef,
		NewType:  newRef,
		Detail: fmt.Sprintf(
			"field %q at index %d references nested struct %s on the old side and %s on the new side; each inner message is a bare 0x7f-terminated sequence with no type tag, so the decoder applies the wrong field layout to the same bytes",
			of.Name, of.Index, oldRef, newRef),
		OldToNew: VerdictIncompatible,
		NewToOld: VerdictIncompatible,
	}
}

// registerEdgeIfStruct links two same-name nested-struct fields to the node of
// the referenced struct, so that its verdict can propagate. Both fields must
// reference the same qualified struct and share their list flag.
func (ctx *compatContext) registerEdgeIfStruct(node *compatNode, of, nf *Field) {
	if of.TypeRef == nil || nf.TypeRef == nil {
		return
	}
	if of.TypeRef.String() != nf.TypeRef.String() {
		return
	}
	if of.TypeList != nf.TypeList {
		return
	}
	target, ok := ctx.nodes[of.TypeRef.String()]
	if !ok {
		return
	}
	node.edges = append(node.edges, compatEdge{
		to: target,
		stepOld: PathStep{
			Struct: node.oldStruct.String(),
			Field:  of.Name,
			Index:  of.Index,
		},
		stepNew: PathStep{
			Struct: node.newStruct.String(),
			Field:  nf.Name,
			Index:  nf.Index,
		},
	})
}

// encodingDesc describes the on-the-wire payload layout used in findings.
func encodingDesc(t string, list bool) string {
	switch t {
	case "bool":
		return "a presence-only header with no payload byte"
	case "uint8":
		return "one fixed payload byte"
	case "uint16":
		return "one or two fixed bytes selected by a 0x80 header flag"
	case "uint32":
		return "an up-to-5-byte big-endian/varint unsigned integer"
	case "uint64":
		return "an up-to-9-byte big-endian/varint unsigned integer"
	case "int32":
		return "a zigzag varint signed integer"
	case "int64":
		return "a zigzag varint signed integer"
	case "float32":
		return "a fixed 4-byte IEEE-754 value"
	case "float64":
		return "a fixed 8-byte IEEE-754 value"
	case "timestamp":
		return "4 or 8 big-endian seconds plus 4 nanosecond bytes"
	case "text", "binary":
		if list {
			return "a varint element count with per-element varint length plus bytes"
		}
		return "a varint length followed by raw bytes"
	default:
		return "a nested 0x7f-terminated message"
	}
}
