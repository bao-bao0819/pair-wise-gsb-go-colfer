package gen

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// mustDecodeHex is test helper shorthand.
func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	data, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// limitErr extracts the budget error or fails the test.
func limitErr(t *testing.T, err error) *ColferLimitError {
	t.Helper()
	var got *ColferLimitError
	if !errors.As(err, &got) {
		t.Fatalf("got error %T: %q, want *ColferLimitError", err, err)
	}
	return got
}

func TestLimitedCumulativeListElements(t *testing.T) {
	// Two small lists: each field is well within ColferListMax, but together
	// they exceed the cumulative budget of 5.
	root := O{
		F32s: []float32{1, 2, 3},
		F64s: []float64{1, 2, 3},
	}
	data, err := root.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	var o O
	_, err = o.UnmarshalLimited(data, ColferDecodeLimits{MaxListElements: 5})
	e := limitErr(t, err)
	if e.Kind != ColferLimitListElements {
		t.Errorf("Kind = %v, want ColferLimitListElements", e.Kind)
	}
	if e.Field != "gen.o.f64s" {
		t.Errorf("Field = %q, want gen.o.f64s", e.Field)
	}
	if e.Limit != 5 || e.Used != 3 || e.Declared != 3 {
		t.Errorf("Limit=%d Used=%d Declared=%d, want 5/3/3", e.Limit, e.Used, e.Declared)
	}
	if !strings.Contains(err.Error(), "list elements") {
		t.Errorf("error %q does not name the budget kind", err)
	}

	// The same message fits with a budget of 6.
	o = O{}
	if err := o.UnmarshalBinaryLimited(data, ColferDecodeLimits{MaxListElements: 6}); err != nil {
		t.Fatalf("boundary budget rejected: %s", err)
	}
}

func TestLimitedCumulativeVarBytes(t *testing.T) {
	// One text field of 2 bytes and one binary field of 3 bytes; each field is
	// fine on its own, but the total of 5 exceeds a budget of 4.
	root := O{S: "ab", A: []byte{1, 2, 3}}
	data, err := root.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	var o O
	_, err = o.UnmarshalLimited(data, ColferDecodeLimits{MaxVarBytes: 4})
	e := limitErr(t, err)
	if e.Kind != ColferLimitVarBytes {
		t.Errorf("Kind = %v, want ColferLimitVarBytes", e.Kind)
	}
	if e.Field != "gen.o.a" {
		t.Errorf("Field = %q, want gen.o.a", e.Field)
	}
	if e.Limit != 4 || e.Used != 2 || e.Declared != 3 {
		t.Errorf("Limit=%d Used=%d Declared=%d, want 4/2/3", e.Limit, e.Used, e.Declared)
	}
	if !strings.Contains(err.Error(), "variable length") {
		t.Errorf("error %q does not name the budget kind", err)
	}
}

// TestLimitedNestedPropagation verifies that the element budget flows from the
// root into every child object, and that lists in multiple children add up.
func TestLimitedNestedPropagation(t *testing.T) {
	root := O{Os: []*O{
		{F32s: []float32{1, 2}},
		{F32s: []float32{3, 4}},
	}}
	data, err := root.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	// Charges: outer list 2, first child 2 (total 4), second child 2 (total 6).
	var o O
	_, err = o.UnmarshalLimited(data, ColferDecodeLimits{MaxListElements: 5})
	e := limitErr(t, err)
	if e.Kind != ColferLimitListElements {
		t.Errorf("Kind = %v, want ColferLimitListElements", e.Kind)
	}
	if e.Field != "gen.o.os[1].f32s" {
		t.Errorf("Field = %q, want gen.o.os[1].f32s", e.Field)
	}
	if e.Used != 4 || e.Declared != 2 {
		t.Errorf("Used=%d Declared=%d, want 4/2", e.Used, e.Declared)
	}

	// Total 6 fits exactly at the boundary.
	o = O{}
	if err := o.UnmarshalBinaryLimited(data, ColferDecodeLimits{MaxListElements: 6}); err != nil {
		t.Fatalf("boundary budget rejected: %s", err)
	}
}

// depthChain builds a serial where each of the d nested O fields (header 10)
// adds another struct level, the deepest struct residing at level d.
func depthChain(d int) []byte {
	b := make([]byte, 0, 2*d+1)
	for n := 0; n < d; n++ {
		b = append(b, 0x0a)
	}
	for n := 0; n <= d; n++ {
		b = append(b, 0x7f)
	}
	return b
}

func TestLimitedDepthBoundary(t *testing.T) {
	for _, gold := range []struct {
		depth, limit int
		ok           bool
	}{
		{1, 1, true}, // deepest level equals the boundary
		{2, 1, false},
		{3, 3, true}, // deepest level equals the boundary
		{4, 3, false},
		{4, 0, true}, // zero disables the depth limit
	} {
		var o O
		err := o.UnmarshalBinaryLimited(depthChain(gold.depth), ColferDecodeLimits{MaxDepth: gold.limit})
		if gold.ok {
			if err != nil {
				t.Errorf("depth=%d limit=%d: %s", gold.depth, gold.limit, err)
			}
			continue
		}
		e := limitErr(t, err)
		if e.Kind != ColferLimitDepth {
			t.Errorf("depth=%d limit=%d: Kind = %v, want ColferLimitDepth", gold.depth, gold.limit, e.Kind)
		}
		if e.Limit != uint64(gold.limit) {
			t.Errorf("depth=%d limit=%d: Limit = %d", gold.depth, gold.limit, e.Limit)
		}
		if !strings.HasPrefix(e.Field, "gen.o.") {
			t.Errorf("depth=%d limit=%d: Field = %q, want schema path", gold.depth, gold.limit, e.Field)
		}
	}
}

// TestLimitedDepthInList checks the field path inside a list element.
func TestLimitedDepthInList(t *testing.T) {
	// Root Os with one child, which itself contains a nested O.
	data := mustDecodeHex(t, "0b010a7f7f")
	var o O
	err := o.UnmarshalBinaryLimited(data, ColferDecodeLimits{MaxDepth: 1})
	e := limitErr(t, err)
	if e.Kind != ColferLimitDepth {
		t.Fatalf("Kind = %v, want ColferLimitDepth", e.Kind)
	}
	if e.Field != "gen.o.os[0].o" {
		t.Errorf("Field = %q, want gen.o.os[0].o", e.Field)
	}
}

// TestLimitedMaliciousLengths feeds huge declared lengths while keeping the
// serial itself tiny, proving that overflow/budget checks precede allocation.
func TestLimitedMaliciousLengths(t *testing.T) {
	const huge = uint64(1) << 63
	hugeVarint := []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}

	cases := []struct {
		name   string
		prefix []byte
		field  string
	}{
		{"text", []byte{0x08}, "gen.o.s"},
		{"float list", []byte{0x10}, "gen.o.f32s"},
		{"text list element", []byte{0x0c, 0x01}, "gen.o.ss[0]"},
	}
	for _, gold := range cases {
		data := append(append([]byte{}, gold.prefix...), hugeVarint...)
		if len(data) > 32 {
			t.Fatalf("%s: test serial is unexpectedly large: %d bytes", gold.name, len(data))
		}

		var o O
		_, err := o.UnmarshalLimited(data, ColferDecodeLimits{})
		e := limitErr(t, err)
		if e.Kind != ColferLimitLengthOverflow {
			t.Errorf("%s: Kind = %v, want ColferLimitLengthOverflow", gold.name, e.Kind)
		}
		if e.Field != gold.field {
			t.Errorf("%s: Field = %q, want %q", gold.name, e.Field, gold.field)
		}
		if e.Declared != huge {
			t.Errorf("%s: Declared = %d, want %d", gold.name, e.Declared, huge)
		}
	}
}

// TestLimitedBudgetNotRestored verifies that elements charged per item keep
// drawing from the budget even when a later item fails, and that a budget can
// be observed in a depleted state after an unsuccessful decode.
func TestLimitedBudgetNotRestored(t *testing.T) {
	root := O{Ss: []string{"aa", "bb", "cc"}}
	data, err := root.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	// 2 + 2 fit, the third string would bring the total to 6.
	budget := newColferBudget(ColferDecodeLimits{MaxVarBytes: 5}, "gen.o")
	var o O
	_, err = o.UnmarshalBudget(data, budget)
	e := limitErr(t, err)
	if e.Field != "gen.o.ss[2]" {
		t.Errorf("Field = %q, want gen.o.ss[2]", e.Field)
	}
	if budget.varBytes != 4 {
		t.Errorf("consumed bytes = %d, want 4 (no restore after failure)", budget.varBytes)
	}

	// Reusing the depleted budget rejects even a tiny value, proving the
	// consumed quota did not reappear.
	_, err = o.UnmarshalBudget(mustDecodeHex(t, "080241427f"), budget)
	e = limitErr(t, err)
	if e.Kind != ColferLimitVarBytes || e.Used != 4 || e.Declared != 2 {
		t.Errorf("reuse: Kind=%v Used=%d Declared=%d, want var-bytes/4/2", e.Kind, e.Used, e.Declared)
	}

	// A fresh decode invocation starts with a fresh budget again.
	o = O{}
	if err := o.UnmarshalBinaryLimited(mustDecodeHex(t, "0801417f"),
		ColferDecodeLimits{MaxVarBytes: 5}); err != nil {
		t.Fatalf("fresh budget rejected: %s", err)
	}
}

// TestLimitedFailureRestoresTarget verifies that the receiver does not expose a
// half-new, half-old state after a failed decode.
func TestLimitedFailureRestoresTarget(t *testing.T) {
	root := O{
		F32s: []float32{1, 2, 3},
		F64s: []float64{1, 2, 3},
	}
	data, err := root.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	o := O{S: "old", A: []byte{9}, F32s: []float32{-1}, F64s: []float64{-2}}
	if _, err := o.UnmarshalLimited(data, ColferDecodeLimits{MaxListElements: 5}); err == nil {
		t.Fatal("expected budget error")
	}
	if o.S != "old" || len(o.A) != 1 || o.A[0] != 9 ||
		len(o.F32s) != 1 || o.F32s[0] != -1 ||
		len(o.F64s) != 1 || o.F64s[0] != -2 {
		t.Errorf("receiver mutated after failure: %+v", o)
	}
}

// TestLimitedZeroCompat verifies that zero-valued limits preserve the legacy
// behaviour for every golden serial.
func TestLimitedZeroCompat(t *testing.T) {
	for _, gold := range newGoldenCases() {
		data, err := hex.DecodeString(gold.serial)
		if err != nil {
			t.Fatal(err)
		}
		var got O
		if err := got.UnmarshalBinaryLimited(data, ColferDecodeLimits{}); err != nil {
			t.Errorf("0x%s: %s", gold.serial, err)
		}
	}
}
