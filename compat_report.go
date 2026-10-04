package colfer

import (
	"fmt"
	"io"
	"strings"
)

// WriteText renders a deterministic, human-readable report to w. Package and
// struct order and finding order come from the already sorted Report.
func (r *Report) WriteText(w io.Writer) error {
	var b strings.Builder

	fmt.Fprintf(&b, "Colfer schema wire-compatibility audit\n")
	fmt.Fprintf(&b, "  old writer -> new reader: %s\n", r.OldToNew)
	fmt.Fprintf(&b, "  new writer -> old reader: %s\n\n", r.NewToOld)

	for _, pr := range r.Packages {
		fmt.Fprintf(&b, "package %s\n", pr.OldName)
		fmt.Fprintf(&b, "  old writer -> new reader: %s\n", pr.OldToNew)
		fmt.Fprintf(&b, "  new writer -> old reader: %s\n", pr.NewToOld)

		for _, f := range pr.Findings {
			writeFindingText(&b, &f, "  ")
		}

		for _, sr := range pr.Structs {
			fmt.Fprintf(&b, "  struct %s\n", sr.QName)
			fmt.Fprintf(&b, "    old writer -> new reader: %s\n", sr.OldToNew)
			fmt.Fprintf(&b, "    new writer -> old reader: %s\n", sr.NewToOld)
			if len(sr.Findings) == 0 {
				fmt.Fprintf(&b, "    no field-level findings\n")
			}
			for i := range sr.Findings {
				writeFindingText(&b, &sr.Findings[i], "    ")
			}
		}
		fmt.Fprintf(&b, "\n")
	}

	_, err := io.WriteString(w, b.String())
	return err
}

func writeFindingText(b *strings.Builder, f *Finding, indent string) {
	fmt.Fprintf(b, "%s- [%s] ", indent, f.Code)

	switch {
	case f.OldField != "" && f.NewField != "":
		fmt.Fprintf(b, "%s: field %q (old index %d) <-> %q (new index %d)",
			f.Struct, f.OldField, f.OldIndex, f.NewField, f.NewIndex)
	case f.OldField != "":
		fmt.Fprintf(b, "%s: old field %q at index %d", f.Struct, f.OldField, f.OldIndex)
	case f.NewField != "":
		fmt.Fprintf(b, "%s: new field %q at index %d", f.Struct, f.NewField, f.NewIndex)
	case f.Struct != "":
		fmt.Fprintf(b, "%s", f.Struct)
	}

	if f.OldType != "" || f.NewType != "" {
		fmt.Fprintf(b, "\n%s      type: %s -> %s", indent, dashOrType(f.OldType), dashOrType(f.NewType))
	}
	if len(f.Path) != 0 {
		fmt.Fprintf(b, "\n%s      path:", indent)
		for _, step := range f.Path {
			fmt.Fprintf(b, "\n%s        - %s", indent, step.String())
		}
	}
	fmt.Fprintf(b, "\n%s      old->new: %s | new->old: %s", indent, f.OldToNew, f.NewToOld)
	fmt.Fprintf(b, "\n%s      %s\n", indent, f.Detail)
}

func dashOrType(s string) string {
	if s == "" {
		return "(absent)"
	}
	return s
}
