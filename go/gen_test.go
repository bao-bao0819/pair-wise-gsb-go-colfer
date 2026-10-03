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

// equalO compares o with want, with a work around for NaN != NaN.
func equalO(o, want *O) bool {
	a, b := *o, *want
	if math.IsNaN(float64(a.F32)) && math.IsNaN(float64(b.F32)) {
		a.F32, b.F32 = 0, 0
	}
	if math.IsNaN(a.F64) && math.IsNaN(b.F64) {
		a.F64, b.F64 = 0, 0
	}
	return reflect.DeepEqual(a, b)
}

func TestDecoderSplits(t *testing.T) {
	for _, gold := range newGoldenCases() {
		data, err := hex.DecodeString(gold.serial)
		if err != nil {
			t.Fatal(err)
		}

		// the one-shot decoder as a reference
		var ref O
		if _, err := ref.Unmarshal(data); err != nil {
			t.Fatalf("0x%s: reference decode: %s", gold.serial, err)
		}

		// split the record at every byte position
		for i := 0; i <= len(data); i++ {
			d := new(ODecoder)
			n, obj, err := d.Feed(data[:i])
			if err != nil {
				t.Errorf("0x%s: split %d: first feed: %s", gold.serial, i, err)
				continue
			}
			if n != i {
				t.Errorf("0x%s: split %d: first feed consumed %d bytes", gold.serial, i, n)
			}
			if i < len(data) && obj != nil {
				t.Errorf("0x%s: split %d: incomplete record delivered", gold.serial, i)
			}
			if obj == nil {
				var n2 int
				n2, obj, err = d.Feed(data[i:])
				if err != nil {
					t.Errorf("0x%s: split %d: second feed: %s", gold.serial, i, err)
					continue
				}
				if n2 != len(data)-i {
					t.Errorf("0x%s: split %d: second feed consumed %d bytes", gold.serial, i, n2)
				}
			}
			if obj == nil {
				t.Errorf("0x%s: split %d: no record delivered", gold.serial, i)
				continue
			}
			if !equalO(obj, &ref) {
				t.Errorf("0x%s: split %d: got %+v, want %+v", gold.serial, i, obj, &ref)
			}
		}

		// feed one byte at a time
		d := new(ODecoder)
		var obj *O
		for i := range data {
			n, o, err := d.Feed(data[i : i+1])
			if err != nil {
				t.Errorf("0x%s: byte %d: %s", gold.serial, i, err)
				break
			}
			if n != 1 {
				t.Errorf("0x%s: byte %d: consumed %d bytes", gold.serial, i, n)
			}
			if o != nil {
				obj = o
			}
		}
		if obj == nil {
			t.Errorf("0x%s: byte feed: no record delivered", gold.serial)
		} else if !equalO(obj, &ref) {
			t.Errorf("0x%s: byte feed: got %+v, want %+v", gold.serial, obj, &ref)
		}
	}
}

func TestDecoderStream(t *testing.T) {
	var stream []byte
	var wants []O
	for _, gold := range newGoldenCases() {
		data, err := hex.DecodeString(gold.serial)
		if err != nil {
			t.Fatal(err)
		}
		stream = append(stream, data...)
		wants = append(wants, gold.object)
	}

	d := new(ODecoder)
	for i, want := range wants {
		n, obj, err := d.Feed(stream)
		if err != nil {
			t.Fatalf("record %d: %s", i, err)
		}
		if obj == nil {
			t.Fatalf("record %d: no record delivered", i)
		}
		if !equalO(obj, &want) {
			t.Errorf("record %d: got %+v, want %+v", i, obj, &want)
		}
		stream = stream[n:]
	}
	if len(stream) != 0 {
		t.Errorf("%d bytes left after %d records", len(stream), len(wants))
	}
	if err := d.Close(); err != nil {
		t.Errorf("close on record boundary: %s", err)
	}
}

func TestDecoderError(t *testing.T) {
	// duplicate field header is an illegal sequence
	data, err := hex.DecodeString("00007f")
	if err != nil {
		t.Fatal(err)
	}

	d := new(ODecoder)
	if _, _, err := d.Feed(data); err != ColferError(1) {
		t.Fatalf("got error %v, want ColferError(1)", err)
	}
	// the failure is sticky
	if _, _, err := d.Feed(data); err != ColferError(1) {
		t.Fatalf("got error %v after failure, want ColferError(1)", err)
	}
	// Reset recovers
	d.Reset()
	n, obj, err := d.Feed(data[2:])
	if err != nil {
		t.Fatalf("got error %s after Reset", err)
	}
	if n != 1 || obj == nil {
		t.Fatalf("got %d bytes and object %v after Reset", n, obj)
	}
}

func TestDecoderSizeMax(t *testing.T) {
	orig := ColferSizeMax
	defer func() {
		ColferSizeMax = orig
	}()

	data, err := hex.DecodeString("0801417f")
	if err != nil {
		t.Fatal(err)
	}

	ColferSizeMax = 4
	d := new(ODecoder)
	if _, _, err := d.Feed(data); err != nil {
		if _, ok := err.(ColferMax); !ok {
			t.Fatalf("got error %T: %q, want ColferMax", err, err)
		}
	} else {
		t.Fatal("no error for record at ColferSizeMax")
	}

	// length prefix beyond the limit
	ColferSizeMax = orig
	d.Reset()
	big := []byte{8, 0xff, 0xff, 0xff, 0xff, 0x7f}
	if _, _, err := d.Feed(big); err != nil {
		if _, ok := err.(ColferMax); !ok {
			t.Fatalf("got error %T: %q, want ColferMax", err, err)
		}
	} else {
		t.Fatal("no error for length prefix beyond ColferSizeMax")
	}
}

func TestDecoderTruncation(t *testing.T) {
	data, err := hex.DecodeString("08026100")
	if err != nil {
		t.Fatal(err)
	}

	d := new(ODecoder)
	n, obj, err := d.Feed(data)
	if err != nil || obj != nil {
		t.Fatalf("got %d, %v, %s for an incomplete record", n, obj, err)
	}
	if err := d.Close(); err != io.ErrUnexpectedEOF {
		t.Fatalf("got close error %v, want io.ErrUnexpectedEOF", err)
	}
	// the failure is sticky
	if _, _, err := d.Feed(data); err != io.ErrUnexpectedEOF {
		t.Fatalf("got error %v after close, want io.ErrUnexpectedEOF", err)
	}
	// Reset recovers
	d.Reset()
	if _, obj, err := d.Feed(append(data, 0x7f)); err != nil || obj == nil {
		t.Fatalf("got %v, %s after Reset", obj, err)
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
