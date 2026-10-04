package gen

import (
	"bytes"
	"encoding/hex"
	"io"
	"io/ioutil"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

type golden struct {
	serial string
	object O
}

func newGoldenCases() []*golden {
	return []*golden{
		{"7f", O{}},
		{"007f", O{B: true}},
		{"01017f", O{U32: 1}},
		{"01ff017f", O{U32: math.MaxUint8}},
		{"01ffff037f", O{U32: math.MaxUint16}},
		{"81ffffffff7f", O{U32: math.MaxUint32}},
		{"02017f", O{U64: 1}},
		{"02ff017f", O{U64: math.MaxUint8}},
		{"02ffff037f", O{U64: math.MaxUint16}},
		{"02ffffffff0f7f", O{U64: math.MaxUint32}},
		{"82ffffffffffffffff7f", O{U64: math.MaxUint64}},
		{"03017f", O{I32: 1}},
		{"83017f", O{I32: -1}},
		{"037f7f", O{I32: math.MaxInt8}},
		{"8380017f", O{I32: math.MinInt8}},
		{"03ffff017f", O{I32: math.MaxInt16}},
		{"838080027f", O{I32: math.MinInt16}},
		{"03ffffffff077f", O{I32: math.MaxInt32}},
		{"8380808080087f", O{I32: math.MinInt32}},
		{"04017f", O{I64: 1}},
		{"84017f", O{I64: -1}},
		{"047f7f", O{I64: math.MaxInt8}},
		{"8480017f", O{I64: math.MinInt8}},
		{"04ffff017f", O{I64: math.MaxInt16}},
		{"848080027f", O{I64: math.MinInt16}},
		{"04ffffffff077f", O{I64: math.MaxInt32}},
		{"8480808080087f", O{I64: math.MinInt32}},
		{"04ffffffffffffffff7f7f", O{I64: math.MaxInt64}},
		{"848080808080808080807f", O{I64: math.MinInt64}},
		{"05000000017f", O{F32: math.SmallestNonzeroFloat32}},
		{"057f7fffff7f", O{F32: math.MaxFloat32}},
		{"057fc000007f", O{F32: float32(math.NaN())}},
		{"0600000000000000017f", O{F64: math.SmallestNonzeroFloat64}},
		{"067fefffffffffffff7f", O{F64: math.MaxFloat64}},
		{"067ff80000000000017f", O{F64: math.NaN()}},
		{"0755ef312a2e5da4e77f", O{T: time.Unix(1441739050, 777888999).In(time.UTC)}},
		{"87000007dba8218000000003e87f", O{T: time.Unix(864e10, 1000).In(time.UTC)}},
		{"87fffff82457de8000000003e97f", O{T: time.Unix(-864e10, 1001).In(time.UTC)}},
		{"87ffffffffffffffff2e5da4e77f", O{T: time.Unix(-1, 777888999).In(time.UTC)}},
		{"0801417f", O{S: "A"}},
		{"080261007f", O{S: "a\x00"}},
		{"0809c280e0a080f09080807f", O{S: "\u0080\u0800\U00010000"}},
		{"08800120202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020202020207f", O{S: strings.Repeat(" ", 128)}},
		{"0901ff7f", O{A: []byte{math.MaxUint8}}},
		{"090202007f", O{A: []byte{2, 0}}},
		{"09c0010909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909090909097f", O{A: bytes.Repeat([]byte{9}, 192)}},
		{"0a7f7f", O{O: &O{}}},
		{"0a007f7f", O{O: &O{B: true}}},
		{"0b01007f7f", O{Os: []*O{{B: true}}}},
		{"0b027f7f7f", O{Os: []*O{{}, {}}}},
		{"0c0300016101627f", O{Ss: []string{"", "a", "b"}}},
		{"0d0201000201027f", O{As: [][]byte{{0}, {1, 2}}}},
		{"0e017f", O{U8: 1}},
		{"0eff7f", O{U8: math.MaxUint8}},
		{"8f017f", O{U16: 1}},
		{"0fffff7f", O{U16: math.MaxUint16}},
		{"1002000000003f8000007f", O{F32s: []float32{0, 1}}},
		{"11014058c000000000007f", O{F64s: []float64{99}}},
	}
}

func TestMarshal(t *testing.T) {
	for _, gold := range newGoldenCases() {
		data, err := gold.object.MarshalBinary()
		if err != nil {
			t.Errorf("0x%s: %s", gold.serial, err)
			continue
		}
		if got := hex.EncodeToString(data); got != gold.serial {
			t.Errorf("Got 0x%s, want 0x%s", got, gold.serial)
		}
	}
}

func TestUnmarshal(t *testing.T) {
	for _, gold := range newGoldenCases() {
		data, err := hex.DecodeString(gold.serial)
		if err != nil {
			t.Fatal(err)
		}

		got := O{}
		if err := got.UnmarshalBinary(data); err != nil {
			t.Errorf("0x%s: %s", gold.serial, err)
			continue
		}

		// work around NaN != NaN
		a, b := got, gold.object
		if math.IsNaN(float64(a.F32)) && math.IsNaN(float64(b.F32)) {
			a.F32, b.F32 = 0, 0
		}
		if math.IsNaN(a.F64) && math.IsNaN(b.F64) {
			a.F64, b.F64 = 0, 0
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("0x%s: got %+v, want %+v", gold.serial, got, gold.object)
		}
	}
}

func TestUnmarshalEOF(t *testing.T) {
	for _, gold := range newGoldenCases() {
		data, err := hex.DecodeString(gold.serial)
		if err != nil {
			t.Fatal(err)
		}

		for i := range data {
			incomplete := data[:i]
			if _, err := new(O).Unmarshal(incomplete); err != io.EOF {
				t.Errorf("0x%s: got error %T: %q", hex.EncodeToString(incomplete), err, err)
			}
		}
	}
}

func TestViewUnmarshal(t *testing.T) {
	for _, gold := range newGoldenCases() {
		data, err := hex.DecodeString(gold.serial)
		if err != nil {
			t.Fatal(err)
		}

		view, n, err := UnmarshalOView(data)
		if err != nil {
			t.Errorf("0x%s: %s", gold.serial, err)
			continue
		}
		if n != len(data) {
			t.Errorf("0x%s: got %d bytes read, want %d", gold.serial, n, len(data))
		}

		// work around NaN != NaN
		got, want := *view.Detach(), gold.object
		if math.IsNaN(float64(got.F32)) && math.IsNaN(float64(want.F32)) {
			got.F32, want.F32 = 0, 0
		}
		if math.IsNaN(got.F64) && math.IsNaN(want.F64) {
			got.F64, want.F64 = 0, 0
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("0x%s: got %+v, want %+v", gold.serial, got, want)
		}
	}
}

func TestViewBorrow(t *testing.T) {
	want := O{
		S:  "keep me",
		A:  []byte{1, 2, 3},
		O:  &O{A: []byte{4, 5}},
		Os: []*O{{A: []byte{6}}, {A: []byte{7, 8}}},
		As: [][]byte{{9}, {10, 11}},
	}
	data, err := want.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	view, _, err := UnmarshalOView(data)
	if err != nil {
		t.Fatal(err)
	}

	// flip all source content
	for i := range data {
		data[i] ^= 0xff
	}

	// binaries follow the source buffer, nesting included
	borrowed := [][]byte{view.A, view.O.A, view.Os[0].A, view.Os[1].A, view.As[0], view.As[1]}
	orig := [][]byte{want.A, want.O.A, want.Os[0].A, want.Os[1].A, want.As[0], want.As[1]}
	for i, got := range borrowed {
		for j, b := range got {
			if want := orig[i][j] ^ 0xff; b != want {
				t.Errorf("borrowed field %d byte %d: got %#x, want %#x after source flip", i, j, b, want)
			}
		}
	}

	// text remains a safe copy
	if view.S != want.S {
		t.Errorf("text changed with source: got %q, want %q", view.S, want.S)
	}
}

func TestViewDetach(t *testing.T) {
	want := O{
		B:    true,
		U32:  1,
		S:    "text",
		A:    []byte{1, 2, 3},
		O:    &O{A: []byte{4}},
		Os:   []*O{{A: []byte{5}}, {}},
		Ss:   []string{"a", "b"},
		As:   [][]byte{{6}, {7, 8}},
		F32s: []float32{1.5},
	}
	data, err := want.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	view, _, err := UnmarshalOView(data)
	if err != nil {
		t.Fatal(err)
	}
	got := view.Detach()

	// destroy the source
	for i := range data {
		data[i] = 0
	}

	if !reflect.DeepEqual(*got, want) {
		t.Errorf("got %+v, want %+v", *got, want)
	}

	var nilView *OView
	if nilView.Detach() != nil {
		t.Error("Detach on nil view did not return nil")
	}
}

func TestViewSliceCap(t *testing.T) {
	o := O{
		A:  []byte{1, 2, 3},
		As: [][]byte{{4}, {5, 6}},
		S:  "next",
	}
	data, err := o.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := append([]byte(nil), data...)

	view, _, err := UnmarshalOView(data)
	if err != nil {
		t.Fatal(err)
	}

	borrowed := []struct {
		name string
		b    []byte
	}{
		{"A", view.A},
		{"As[0]", view.As[0]},
		{"As[1]", view.As[1]},
	}
	for _, x := range borrowed {
		if cap(x.b) != len(x.b) {
			t.Errorf("%s: got capacity %d, want %d", x.name, cap(x.b), len(x.b))
		}
	}

	// appends must not write into the source buffer
	view.A = append(view.A, 7)
	view.As[0] = append(view.As[0], 8)
	view.As[1] = append(view.As[1], 9, 10)
	if !bytes.Equal(data, snapshot) {
		t.Error("append on a view field wrote into the source buffer")
	}
	if !bytes.Equal(view.A, []byte{1, 2, 3, 7}) {
		t.Errorf("A: got %v after append", view.A)
	}
}

func TestViewError(t *testing.T) {
	for _, gold := range newGoldenCases() {
		data, err := hex.DecodeString(gold.serial)
		if err != nil {
			t.Fatal(err)
		}

		for i := range data {
			view, _, err := UnmarshalOView(data[:i])
			if err != io.EOF {
				t.Errorf("0x%s: got error %T: %q", hex.EncodeToString(data[:i]), err, err)
			}
			if view != nil {
				t.Errorf("0x%s: view published on error", hex.EncodeToString(data[:i]))
			}
		}
	}

	// size limit breach
	if view, _, err := UnmarshalOView([]byte{0x09, 0xff, 0xff, 0xff, 0xff, 0x0f}); err == nil || view != nil {
		t.Errorf("got view %+v and error %v for oversized binary", view, err)
	}

	// unknown header
	if view, _, err := UnmarshalOView([]byte{0x37, 0x7f}); err == nil || view != nil {
		t.Errorf("got view %+v and error %v for unknown header", view, err)
	}
}

func TestUnmarshalSizeMax(t *testing.T) {
	orig := ColferSizeMax
	defer func() {
		ColferSizeMax = orig
	}()

	for _, gold := range newGoldenCases() {
		data, err := hex.DecodeString(gold.serial)
		if err != nil {
			t.Fatal(err)
		}

		for ColferSizeMax = range data {
			// not supported
			if ColferSizeMax == 0 {
				continue
			}

			for i := range data {
				// cutoff on or after max
				if i+1 < ColferSizeMax {
					continue
				}
				part := data[:i+1]

				switch _, err := new(O).Unmarshal(part); err.(type) {
				case ColferMax:
					continue // pass
				case nil:
					t.Errorf("0x%s: no error with ColferSizeMax=%d", hex.EncodeToString(part), ColferSizeMax)
				default:
					t.Errorf("0x%s: got error %T with ColferSizeMax=%d: %q", hex.EncodeToString(part), err, ColferSizeMax, err)
				}
				break
			}
		}
	}
}

// TestFuzzSeed updates the initial input corpus for fuzz testing.
func TestFuzzSeed(t *testing.T) {
	for _, gold := range newGoldenCases() {
		data, err := gold.object.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if ioutil.WriteFile("../testdata/corpus/seed"+gold.serial, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func FuzzO(f *testing.F) {
	for _, gold := range newGoldenCases() {
		bytes, err := hex.DecodeString(gold.serial)
		if err != nil {
			f.Fatalf("malformed golden serial %q: %s", gold.serial, err)
		}
		f.Add(bytes)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		o := new(O)
		err := o.UnmarshalBinary(data)
		if err != nil {
			return
		}
		_, err = o.MarshalBinary()
		if err != nil {
			t.Fatal("marshal error:", err)
		}

		view, _, err := UnmarshalOView(data)
		if err != nil {
			t.Fatal("view decode error after successful unmarshal:", err)
		}
		if _, err := view.Detach().MarshalBinary(); err != nil {
			t.Fatal("marshal error after detach:", err)
		}
	})
}
