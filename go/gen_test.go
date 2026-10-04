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

func TestUnmarshalView(t *testing.T) {
	for _, gold := range newGoldenCases() {
		data, err := hex.DecodeString(gold.serial)
		if err != nil {
			t.Fatal(err)
		}

		view := new(OView)
		n, err := view.UnmarshalView(data)
		if err != nil {
			t.Errorf("0x%s: %s", gold.serial, err)
			continue
		}
		if n != len(data) {
			t.Errorf("0x%s: got %d bytes, want %d", gold.serial, n, len(data))
		}

		// work around NaN != NaN
		got := view.Detach()
		a, b := *got, gold.object
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

// TestViewBorrow verifies that binary fields, including the nested and list
// variants, alias the source buffer directly instead of taking a copy.
func TestViewBorrow(t *testing.T) {
	object := O{
		A:  []byte{0x51, 0x52},
		O:  &O{A: []byte{0x61, 0x62}},
		As: [][]byte{{0x71, 0x72}, {0x73, 0x74}},
		Os: []*O{{A: []byte{0x81, 0x82}}, {A: []byte{0x83, 0x84}}},
		S:  "keep text copied",
	}
	data, err := object.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	view := new(OView)
	if err := view.UnmarshalViewBinary(data); err != nil {
		t.Fatal(err)
	}

	type alias struct {
		name   string
		b      []byte
		offset int
		first  byte
	}
	borrowed := []alias{
		{"A", view.A, bytes.IndexByte(data, 0x51), 0x51},
		{"O.A", view.O.A, bytes.IndexByte(data, 0x61), 0x61},
		{"As[0]", view.As[0], bytes.Index(data, []byte{0x71, 0x72}), 0x71},
		{"As[1]", view.As[1], bytes.Index(data, []byte{0x73, 0x74}), 0x73},
		{"Os[0].A", view.Os[0].A, bytes.Index(data, []byte{0x81, 0x82}), 0x81},
		{"Os[1].A", view.Os[1].A, bytes.Index(data, []byte{0x83, 0x84}), 0x83},
	}

	for _, a := range borrowed {
		if a.offset < 0 {
			t.Errorf("%s: marker not found in serial", a.name)
			continue
		}
		// The slice header must point into data itself.
		if &a.b[0] != &data[a.offset] {
			t.Errorf("%s: binary copied instead of borrowed", a.name)
			continue
		}
		if cap(a.b) != len(a.b) {
			t.Errorf("%s: cap %d, want %d (field boundary leak)", a.name, cap(a.b), len(a.b))
		}

		// Mutating the source must be visible through the view.
		data[a.offset] = 0x01
		if a.b[0] != 0x01 {
			t.Errorf("%s: source mutation not visible", a.name)
		}
		data[a.offset] = a.first

		// Appending must allocate; the next field in data must survive.
		grown := append(a.b, 0xee)
		if &grown[0] == &data[a.offset] {
			t.Errorf("%s: append stayed in source buffer", a.name)
		}
		if data[a.offset] != a.first {
			t.Errorf("%s: append overwrote source", a.name)
		}
	}

	// A list slice itself is capped too: no append past its elements.
	if cap(view.As) != len(view.As) {
		t.Errorf("As: cap %d, want %d", cap(view.As), len(view.As))
	}
	if cap(view.Os) != len(view.Os) {
		t.Errorf("Os: cap %d, want %d", cap(view.Os), len(view.Os))
	}

	// Text follows string semantics: it must not alias data.
	if offset := bytes.Index(data, []byte("keep text copied")); offset < 0 {
		t.Fatal("text marker not found")
	} else {
		data[offset] = 'X'
		if view.S != "keep text copied" {
			t.Errorf("S: text changed with source, got %q", view.S)
		}
		data[offset] = 'k'
	}
}

// TestViewDetach verifies that a detached value owns all its bytes and stays
// valid after the source buffer is mutated.
func TestViewDetach(t *testing.T) {
	object := O{
		A:  []byte{0x51, 0x52},
		O:  &O{A: []byte{0x61, 0x62}},
		As: [][]byte{{0x71, 0x72}, {0x73, 0x74}},
		Os: []*O{{A: []byte{0x81, 0x82}}, {A: []byte{0x83, 0x84}}},
		S:  "independent",
	}
	data, err := object.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	view := new(OView)
	if err := view.UnmarshalViewBinary(data); err != nil {
		t.Fatal(err)
	}
	got := view.Detach()

	if !reflect.DeepEqual(*got, object) {
		t.Errorf("got %+v, want %+v", got, object)
	}

	// Corrupt the source after detachment.
	for i := range data {
		data[i] = 0
	}

	if !reflect.DeepEqual(*got, object) {
		t.Errorf("detached value changed: got %+v, want %+v", got, object)
	}
	if got.A[0] != 0x51 || got.O.A[0] != 0x61 || got.As[1][0] != 0x73 || got.Os[1].A[1] != 0x84 {
		t.Error("detached binary still aliases source")
	}
}

// TestViewErrorAtomic verifies that a failed publish leaves the target
// untouched, i.e. no partial view is ever observable.
func TestViewErrorAtomic(t *testing.T) {
	object := O{
		A:  []byte{0x51, 0x52},
		O:  &O{A: []byte{0x61, 0x62}},
		As: [][]byte{{0x71, 0x72}},
		S:  "sentinel",
	}
	data, err := object.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	view := new(OView)
	if err := view.UnmarshalViewBinary(data); err != nil {
		t.Fatal(err)
	}

	for _, cut := range []int{0, 1, 2, len(data) - 1} {
		reused := *view // pre-populated target
		if _, err := reused.UnmarshalView(data[:cut]); err == nil {
			t.Errorf("cut %d: expected error", cut)
		}
		if reused.S != view.S || reused.A == nil || &reused.A[0] != &view.A[0] {
			t.Errorf("cut %d: target partially overwritten on error", cut)
		}
	}
}

// TestUnmarshalOwnership verifies the original copy semantics: unmarshalled
// binaries never alias the input.
func TestUnmarshalOwnership(t *testing.T) {
	object := O{
		A:  []byte{0x51, 0x52},
		O:  &O{A: []byte{0x61, 0x62}},
		As: [][]byte{{0x71, 0x72}},
	}
	data, err := object.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	got := new(O)
	if err := got.UnmarshalBinary(data); err != nil {
		t.Fatal(err)
	}
	for i := range data {
		data[i] = 0
	}
	if !reflect.DeepEqual(*got, object) {
		t.Errorf("owned value changed after source mutation: got %+v, want %+v", got, object)
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

func TestUnmarshalViewEOF(t *testing.T) {
	for _, gold := range newGoldenCases() {
		data, err := hex.DecodeString(gold.serial)
		if err != nil {
			t.Fatal(err)
		}

		for i := range data {
			incomplete := data[:i]
			if _, err := new(OView).UnmarshalView(incomplete); err != io.EOF {
				t.Errorf("0x%s: got error %T: %q", hex.EncodeToString(incomplete), err, err)
			}
		}
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

		v := new(OView)
		if err := v.UnmarshalViewBinary(data); err != nil {
			t.Fatal("view unmarshal error:", err)
		}
		if _, err = v.Detach().MarshalBinary(); err != nil {
			t.Fatal("detached marshal error:", err)
		}
	})
}
