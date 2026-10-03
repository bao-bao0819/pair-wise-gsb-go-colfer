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
	})
}

// FuzzODecoder verifies the incremental decoder against the one-shot
// Unmarshal on arbitrary input.
func FuzzODecoder(f *testing.F) {
	for _, gold := range newGoldenCases() {
		bytes, err := hex.DecodeString(gold.serial)
		if err != nil {
			f.Fatalf("malformed golden serial %q: %s", gold.serial, err)
		}
		f.Add(bytes)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		want := new(O)
		wn, werr := want.Unmarshal(data)

		d := new(ODecoder)
		gn, got, gerr := d.Feed(data)

		if werr != nil {
			if got != nil {
				t.Fatalf("0x%x: decoder completed a record where unmarshal failed with %v", data, werr)
			}
			if _, ok := werr.(ColferError); ok {
				if _, ok := gerr.(ColferError); !ok {
					t.Fatalf("0x%x: unmarshal got ColferError, decoder got %v", data, gerr)
				}
			}
			return
		}

		if gerr != nil {
			t.Fatalf("0x%x: unmarshal read %d bytes, decoder got error %v", data, wn, gerr)
		}
		if got == nil {
			t.Fatalf("0x%x: unmarshal read %d bytes, decoder needs more", data, wn)
		}
		if gn != wn {
			t.Fatalf("0x%x: unmarshal read %d bytes, decoder read %d", data, wn, gn)
		}
		if !equalO(got, *want) {
			t.Fatalf("0x%x: unmarshal got %+v, decoder got %+v", data, want, got)
		}
	})
}

// equalO compares o to the golden object, working around NaN != NaN.
func equalO(got *O, want O) bool {
	a, b := *got, want
	zeroNaN(&a)
	zeroNaN(&b)
	return reflect.DeepEqual(a, b)
}

// zeroNaN normalizes all NaN values in o, recursively.
func zeroNaN(o *O) {
	if math.IsNaN(float64(o.F32)) {
		o.F32 = 0
	}
	if math.IsNaN(o.F64) {
		o.F64 = 0
	}
	for i, v := range o.F32s {
		if math.IsNaN(float64(v)) {
			o.F32s[i] = 0
		}
	}
	for i, v := range o.F64s {
		if math.IsNaN(v) {
			o.F64s[i] = 0
		}
	}
	if o.O != nil {
		zeroNaN(o.O)
	}
	for _, e := range o.Os {
		if e != nil {
			zeroNaN(e)
		}
	}
}

// TestDecoderSplit feeds every golden record in two fragments, split at every
// byte position. No object may surface before the record completes.
func TestDecoderSplit(t *testing.T) {
	for _, gold := range newGoldenCases() {
		data, err := hex.DecodeString(gold.serial)
		if err != nil {
			t.Fatal(err)
		}

		for cut := 0; cut <= len(data); cut++ {
			d := new(ODecoder)
			n1, o1, err := d.Feed(data[:cut])
			if err != nil {
				t.Errorf("0x%s cut %d: first feed: %s", gold.serial, cut, err)
				continue
			}
			if n1 != cut {
				t.Errorf("0x%s cut %d: first feed consumed %d of %d bytes", gold.serial, cut, n1, cut)
				continue
			}

			var obj *O
			total := n1
			if o1 != nil {
				if cut != len(data) {
					t.Errorf("0x%s cut %d: object completed before the record end", gold.serial, cut)
					continue
				}
				obj = o1
			} else {
				n2, o2, err := d.Feed(data[cut:])
				if err != nil {
					t.Errorf("0x%s cut %d: second feed: %s", gold.serial, cut, err)
					continue
				}
				total += n2
				if o2 == nil {
					t.Errorf("0x%s cut %d: no object after both feeds", gold.serial, cut)
					continue
				}
				obj = o2
			}

			if total != len(data) {
				t.Errorf("0x%s cut %d: consumed %d of %d bytes", gold.serial, cut, total, len(data))
			}
			if !equalO(obj, gold.object) {
				t.Errorf("0x%s cut %d: got %+v, want %+v", gold.serial, cut, obj, gold.object)
			}
		}
	}
}

// TestDecoderStream feeds all golden records concatenated as one stream in
// various chunk sizes, including byte by byte and all at once. Multiple
// records in one fragment must each surface with an exact consume count.
func TestDecoderStream(t *testing.T) {
	golds := newGoldenCases()
	var stream []byte
	for _, gold := range golds {
		data, err := hex.DecodeString(gold.serial)
		if err != nil {
			t.Fatal(err)
		}
		stream = append(stream, data...)
	}

	for _, chunk := range []int{1, 2, 3, 5, 7, 13, 64, len(stream)} {
		d := new(ODecoder)
		var got []*O
		for off := 0; off < len(stream); {
			end := off + chunk
			if end > len(stream) {
				end = len(stream)
			}
			n, o, err := d.Feed(stream[off:end])
			off += n
			if err != nil {
				t.Fatalf("chunk %d: feed error at offset %d: %s", chunk, off, err)
			}
			if o != nil {
				got = append(got, o)
			}
			if n == 0 && o == nil {
				t.Fatalf("chunk %d: no progress at offset %d", chunk, off)
			}
		}
		if err := d.End(); err != nil {
			t.Fatalf("chunk %d: end of stream: %s", chunk, err)
		}

		if len(got) != len(golds) {
			t.Fatalf("chunk %d: got %d objects, want %d", chunk, len(got), len(golds))
		}
		for i, o := range got {
			if !equalO(o, golds[i].object) {
				t.Errorf("chunk %d: record %d: got %+v, want %+v", chunk, i, o, golds[i].object)
			}
		}
	}
}

// TestDecoderError verifies the error taxonomy: illegal field sequences,
// limit breaches and truncation on explicit end of input are distinct, and
// any error sticks until Reset.
func TestDecoderError(t *testing.T) {
	t.Run("Header", func(t *testing.T) {
		d := new(ODecoder)
		// 0x7e is not a field of O
		if _, _, err := d.Feed([]byte{0x7e}); err != ColferError(0) {
			t.Fatalf("got error %v, want ColferError(0)", err)
		}
		// out of order: field 0 after field 1
		d.Reset()
		if _, _, err := d.Feed([]byte{0x01, 0x01, 0x00}); err != ColferError(2) {
			t.Fatalf("got error %v, want ColferError(2)", err)
		}
		// error sticks
		if _, _, err := d.Feed([]byte{0x7f}); err != ColferError(2) {
			t.Fatalf("sticky: got error %v, want ColferError(2)", err)
		}
		// Reset recovers
		d.Reset()
		n, o, err := d.Feed([]byte{0x00, 0x7f})
		if err != nil || o == nil || n != 2 {
			t.Fatalf("after Reset: got n=%d, o=%v, err=%v", n, o, err)
		}
		if !o.B {
			t.Error("after Reset: B not set")
		}
	})

	t.Run("Max", func(t *testing.T) {
		defer func(orig int) { ColferSizeMax = orig }(ColferSizeMax)
		ColferSizeMax = 4

		d := new(ODecoder)
		// S = "A" serializes to 4 bytes: one too many
		n, o, err := d.Feed([]byte{0x08, 0x01, 'A', 0x7f})
		if _, ok := err.(ColferMax); !ok {
			t.Fatalf("got error %T: %v, want ColferMax", err, err)
		}
		if o != nil {
			t.Error("object delivered despite the size limit")
		}
		if n != 4 {
			t.Errorf("consumed %d of 4 bytes", n)
		}

		defer func(orig int) { ColferListMax = orig }(ColferListMax)
		ColferListMax = 1
		d.Reset()
		// Os with 2 elements
		_, o2, err2 := d.Feed([]byte{0x0b, 0x02, 0x7f, 0x7f, 0x7f})
		if _, ok := err2.(ColferMax); !ok {
			t.Fatalf("got error %T: %v, want ColferMax", err2, err2)
		}
		if o2 != nil {
			t.Error("object delivered despite the list limit")
		}
	})

	t.Run("End", func(t *testing.T) {
		d := new(ODecoder)
		if err := d.End(); err != nil {
			t.Fatalf("End at record boundary: %s", err)
		}

		// S = "A" without content and terminator
		if _, o, err := d.Feed([]byte{0x08, 0x01}); err != nil || o != nil {
			t.Fatalf("feed: got o=%v, err=%v", o, err)
		}
		if err := d.End(); err != io.ErrUnexpectedEOF {
			t.Fatalf("got error %v, want io.ErrUnexpectedEOF", err)
		}
		// error sticks, even for the remaining record bytes
		if _, _, err := d.Feed([]byte{'A', 0x7f}); err != io.ErrUnexpectedEOF {
			t.Fatalf("sticky: got error %v, want io.ErrUnexpectedEOF", err)
		}
		// Reset recovers
		d.Reset()
		n, o, err := d.Feed([]byte{0x08, 0x01, 'A', 0x7f})
		if err != nil || o == nil || n != 4 {
			t.Fatalf("after Reset: got n=%d, o=%v, err=%v", n, o, err)
		}
		if o.S != "A" {
			t.Errorf("after Reset: got S %q, want %q", o.S, "A")
		}
		if err := d.End(); err != nil {
			t.Fatalf("End at record boundary: %s", err)
		}
	})
}
