package gen

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"reflect"
	"strconv"
	"testing"
)

func marshalGolden(t *testing.T, o O) []byte {
	t.Helper()
	data, err := o.MarshalBinary()
	if err != nil {
		t.Fatal("marshal:", err)
	}
	return data
}

// TestUnmarshalWithOptionsListTotal verifies that list elements charge
// a cumulative budget across fields: each field on its own is within
// the limit, yet the message as a whole is not.
func TestUnmarshalWithOptionsListTotal(t *testing.T) {
	msg := O{Ss: []string{"a", "b", "c"}, As: [][]byte{{1}, {2}}}
	data := marshalGolden(t, msg)

	var o O
	_, err := o.UnmarshalWithOptions(data, &UnmarshalOptions{ListTotalMax: 4})
	berr, ok := err.(ColferBudgetError)
	if !ok {
		t.Fatalf("got error %T: %q, want ColferBudgetError", err, err)
	}
	if berr.Kind != "list" {
		t.Errorf("got budget kind %q, want %q", berr.Kind, "list")
	}
	if berr.Field != "gen.o.as" {
		t.Errorf("got field path %q, want %q", berr.Field, "gen.o.as")
	}
	if berr.Limit != 4 {
		t.Errorf("got limit %d, want 4", berr.Limit)
	}

	// the exact total is allowed
	var ok2 O
	if _, err := ok2.UnmarshalWithOptions(data, &UnmarshalOptions{ListTotalMax: 5}); err != nil {
		t.Fatal("exact budget rejected:", err)
	}
	if !reflect.DeepEqual(ok2, msg) {
		t.Errorf("got %+v, want %+v", ok2, msg)
	}
}

// TestUnmarshalWithOptionsListTotalNested verifies that the budget
// reaches nested structures and accumulates across sibling elements.
func TestUnmarshalWithOptionsListTotalNested(t *testing.T) {
	msg := O{Os: []*O{
		{Ss: []string{"a", "b"}},
		{Ss: []string{"c", "d"}},
	}}
	data := marshalGolden(t, msg)

	// 2 Os elements + 2 + 2 Ss elements = 6
	if _, err := new(O).UnmarshalWithOptions(data, &UnmarshalOptions{ListTotalMax: 6}); err != nil {
		t.Fatal("exact budget rejected:", err)
	}

	_, err := new(O).UnmarshalWithOptions(data, &UnmarshalOptions{ListTotalMax: 5})
	berr, ok := err.(ColferBudgetError)
	if !ok {
		t.Fatalf("got error %T: %q, want ColferBudgetError", err, err)
	}
	if berr.Kind != "list" {
		t.Errorf("got budget kind %q, want %q", berr.Kind, "list")
	}
	if want := "gen.o.os/gen.o.ss"; berr.Field != want {
		t.Errorf("got field path %q, want %q", berr.Field, want)
	}
}

// TestUnmarshalWithOptionsVarBytes verifies that text and binary
// content charge a cumulative byte budget across fields, including
// content of nested structures.
func TestUnmarshalWithOptionsVarBytes(t *testing.T) {
	msg := O{S: "hello", A: []byte("world"), O: &O{S: "!"}}
	data := marshalGolden(t, msg)

	// 5 + 5 + 1 = 11 bytes
	if _, err := new(O).UnmarshalWithOptions(data, &UnmarshalOptions{VarBytesMax: 11}); err != nil {
		t.Fatal("exact budget rejected:", err)
	}

	// each field on its own is within the limit
	_, err := new(O).UnmarshalWithOptions(data, &UnmarshalOptions{VarBytesMax: 10})
	berr, ok := err.(ColferBudgetError)
	if !ok {
		t.Fatalf("got error %T: %q, want ColferBudgetError", err, err)
	}
	if berr.Kind != "bytes" {
		t.Errorf("got budget kind %q, want %q", berr.Kind, "bytes")
	}
	if want := "gen.o.o/gen.o.s"; berr.Field != want {
		t.Errorf("got field path %q, want %q", berr.Field, want)
	}
}

// TestUnmarshalWithOptionsDepth verifies the nesting limit, including
// the boundary where the depth equals the limit exactly.
func TestUnmarshalWithOptionsDepth(t *testing.T) {
	msg := O{O: &O{O: &O{}}}
	data := marshalGolden(t, msg)

	// depth equal to the limit is allowed
	var o O
	if _, err := o.UnmarshalWithOptions(data, &UnmarshalOptions{DepthMax: 2}); err != nil {
		t.Fatal("exact budget rejected:", err)
	}
	if !reflect.DeepEqual(o, msg) {
		t.Errorf("got %+v, want %+v", o, msg)
	}

	_, err := new(O).UnmarshalWithOptions(data, &UnmarshalOptions{DepthMax: 1})
	berr, ok := err.(ColferBudgetError)
	if !ok {
		t.Fatalf("got error %T: %q, want ColferBudgetError", err, err)
	}
	if berr.Kind != "depth" {
		t.Errorf("got budget kind %q, want %q", berr.Kind, "depth")
	}
	if want := "gen.o.o/gen.o.o"; berr.Field != want {
		t.Errorf("got field path %q, want %q", berr.Field, want)
	}
	if berr.Limit != 1 {
		t.Errorf("got limit %d, want 1", berr.Limit)
	}
}

// TestUnmarshalWithOptionsLengthPrefix verifies that declared lengths
// are checked against the budgets before any allocation happens.
func TestUnmarshalWithOptionsLengthPrefix(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("declared length exceeds uint")
	}

	// Raise the classic limits so the budget is the only guard
	// between the declared length and the allocation.
	origSize, origList := ColferSizeMax, ColferListMax
	defer func() {
		ColferSizeMax, ColferListMax = origSize, origList
	}()
	ColferSizeMax, ColferListMax = math.MaxInt64, math.MaxInt64

	var prefix [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(prefix[:], 1<<40)
	huge := prefix[:n]

	tests := []struct {
		name   string
		header byte
		opts   *UnmarshalOptions
		kind   string
	}{
		{"text", 0x08, &UnmarshalOptions{VarBytesMax: 1024}, "bytes"},
		{"binary", 0x09, &UnmarshalOptions{VarBytesMax: 1024}, "bytes"},
		{"text-list", 0x0c, &UnmarshalOptions{ListTotalMax: 8}, "list"},
		{"struct-list", 0x0b, &UnmarshalOptions{ListTotalMax: 8}, "list"},
	}
	for _, test := range tests {
		data := append([]byte{test.header}, huge...)

		_, err := new(O).UnmarshalWithOptions(data, test.opts)
		berr, ok := err.(ColferBudgetError)
		if !ok {
			t.Errorf("%s: got error %T: %q, want ColferBudgetError", test.name, err, err)
			continue
		}
		if berr.Kind != test.kind {
			t.Errorf("%s: got budget kind %q, want %q", test.name, berr.Kind, test.kind)
		}
	}
}

// TestUnmarshalLengthPrefixClassic verifies that the classic limits
// reject malicious length prefixes before any allocation happens.
func TestUnmarshalLengthPrefixClassic(t *testing.T) {
	var prefix [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(prefix[:], 1<<40)
	huge := prefix[:n]

	for _, header := range []byte{0x08, 0x09, 0x0b, 0x0c} {
		data := append([]byte{header}, huge...)
		if _, err := new(O).Unmarshal(data); err == nil {
			t.Errorf("header 0x%x: no error", header)
		} else if _, ok := err.(ColferMax); !ok {
			t.Errorf("header 0x%x: got error %T: %q, want ColferMax", header, err, err)
		}
	}
}

// TestUnmarshalWithOptionsAtomic verifies that a failed decode leaves
// the target object untouched.
func TestUnmarshalWithOptionsAtomic(t *testing.T) {
	data := marshalGolden(t, O{S: "new", Ss: []string{"x", "y"}})

	o := O{S: "keep", U32: 42}
	if _, err := o.UnmarshalWithOptions(data, &UnmarshalOptions{ListTotalMax: 1}); err == nil {
		t.Fatal("no budget error")
	}
	want := O{S: "keep", U32: 42}
	if !reflect.DeepEqual(o, want) {
		t.Errorf("got %+v after failure, want %+v", o, want)
	}
}

// TestUnmarshalWithOptionsZeroValue verifies that a nil or zero
// UnmarshalOptions decodes exactly like Unmarshal.
func TestUnmarshalWithOptionsZeroValue(t *testing.T) {
	for _, gold := range newGoldenCases() {
		data, err := hex.DecodeString(gold.serial)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := new(O).UnmarshalWithOptions(data, nil); err != nil {
			t.Errorf("0x%s: nil options: %s", gold.serial, err)
		}
		if _, err := new(O).UnmarshalWithOptions(data, new(UnmarshalOptions)); err != nil {
			t.Errorf("0x%s: zero options: %s", gold.serial, err)
		}
	}
}
