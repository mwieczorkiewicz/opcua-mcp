package store

import (
	"fmt"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// TestEncodeDecodeRoundTrip is a property-based test (PBT-02) covering
// encode/decode for every scalar type in the supported closed set, plus
// nested arrays. It complements (not replaces) the example-based tests
// below, per PBT-10.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Run("bool", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			v := rapid.Bool().Draw(rt, "v")
			assertRoundTrip(rt, v)
		})
	})
	t.Run("int8", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			assertRoundTrip(rt, rapid.Int8().Draw(rt, "v"))
		})
	})
	t.Run("int16", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			assertRoundTrip(rt, rapid.Int16().Draw(rt, "v"))
		})
	})
	t.Run("int32", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			assertRoundTrip(rt, rapid.Int32().Draw(rt, "v"))
		})
	})
	t.Run("int64", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			assertRoundTrip(rt, rapid.Int64().Draw(rt, "v"))
		})
	})
	t.Run("uint8", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			assertRoundTrip(rt, rapid.Uint8().Draw(rt, "v"))
		})
	})
	t.Run("uint16", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			assertRoundTrip(rt, rapid.Uint16().Draw(rt, "v"))
		})
	})
	t.Run("uint32", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			assertRoundTrip(rt, rapid.Uint32().Draw(rt, "v"))
		})
	})
	t.Run("uint64", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			assertRoundTrip(rt, rapid.Uint64().Draw(rt, "v"))
		})
	})
	t.Run("float32", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			assertRoundTrip(rt, rapid.Float32().Draw(rt, "v"))
		})
	})
	t.Run("float64", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			assertRoundTrip(rt, rapid.Float64().Draw(rt, "v"))
		})
	})
	t.Run("string", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			assertRoundTrip(rt, rapid.String().Draw(rt, "v"))
		})
	})
	t.Run("bytes", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			assertRoundTrip(rt, []byte(rapid.String().Draw(rt, "v")))
		})
	})
	t.Run("time", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			// time.Time round-trips through JSON as RFC3339 (nanosecond
			// precision, UTC-normalized) - generate within that fidelity.
			sec := rapid.Int64Range(0, 4102444800).Draw(rt, "sec") // 1970-2100
			v := time.Unix(sec, 0).UTC()
			assertRoundTrip(rt, v)
		})
	})
	t.Run("array", func(t *testing.T) {
		rapid.Check(t, func(rt *rapid.T) {
			elems := rapid.SliceOfN(rapid.Int32(), 0, 10).Draw(rt, "v")
			v := make([]interface{}, len(elems))
			for i, e := range elems {
				v[i] = e
			}
			assertRoundTrip(rt, v)
		})
	})
}

// assertRoundTrip encodes then decodes v, asserting the result equals v
// exactly (same value and dynamic type).
func assertRoundTrip(rt *rapid.T, v interface{}) {
	rt.Helper()

	encoded, err := encode(v)
	if err != nil {
		rt.Fatalf("encode(%#v) error: %v", v, err)
	}

	decoded, err := decode(encoded)
	if err != nil {
		rt.Fatalf("decode(encode(%#v)) error: %v", v, err)
	}

	if arr, ok := v.([]interface{}); ok {
		decodedArr, ok := decoded.([]interface{})
		if !ok || len(decodedArr) != len(arr) {
			rt.Fatalf("decode(encode(%#v)) = %#v, want matching array", v, decoded)
		}
		for i := range arr {
			if fmt.Sprintf("%v", arr[i]) != fmt.Sprintf("%v", decodedArr[i]) {
				rt.Fatalf("decode(encode(...))[%d] = %#v, want %#v", i, decodedArr[i], arr[i])
			}
		}
		return
	}

	if t, ok := v.(time.Time); ok {
		decodedTime, ok := decoded.(time.Time)
		if !ok || !decodedTime.Equal(t) {
			rt.Fatalf("decode(encode(%#v)) = %#v, want an equal time.Time", v, decoded)
		}
		return
	}

	if fmt.Sprintf("%#v", decoded) != fmt.Sprintf("%#v", v) {
		rt.Fatalf("decode(encode(%#v)) = %#v, want exact match (including type)", v, decoded)
	}
}

// TestEncodeUnsupportedTypeFailsFast is an example-based test (PBT-10:
// PBT complements but doesn't replace example-based tests) covering
// business-rules.md BR-2's fail-fast requirement: an unsupported dynamic
// type must return an error, never silently degrade fidelity via a default
// JSON encoding fallback.
func TestEncodeUnsupportedTypeFailsFast(t *testing.T) {
	tests := []struct {
		name string
		v    interface{}
	}{
		{"map", map[string]int{"a": 1}},
		{"struct", struct{ X int }{X: 1}},
		{"nil", nil},
		{"chan", make(chan int)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := encode(tt.v)
			if err == nil {
				t.Errorf("encode(%#v) expected an error, got nil", tt.v)
			}
		})
	}
}

// TestDecodeUnknownKindFailsFast covers the decode side of BR-2's fail-fast rule.
func TestDecodeUnknownKindFailsFast(t *testing.T) {
	_, err := decode(encodedValue{Kind: "not-a-real-kind"})
	if err == nil {
		t.Error("decode() with an unknown kind expected an error, got nil")
	}
}
