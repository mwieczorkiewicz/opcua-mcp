package opcua

import (
	"fmt"
	"math"
	"reflect"

	"github.com/gopcua/opcua/ua"
)

var byteSliceType = reflect.TypeOf([]byte(nil))

// NormalizeReadValue converts a decoded ua.Variant value (as returned by
// ua.Variant.Value()) into a form that always marshals to JSON safely and
// never silently discards data as a bare null. Without this, several
// gopcua wire types break downstream consumers: typed array slices
// ([]int32, []float64, ...) aren't accepted by the persistent cache's
// closed-set encoder; NaN/Inf floats fail encoding/json outright; NodeID/
// GUID/QualifiedName/LocalizedText/StatusCode wrapper types marshal into
// awkward reflection-based struct dumps instead of their natural string
// form; and an *ua.ExtensionObject for a DataType gopcua has no registered
// decoder for (any vendor/custom structure outside its ~308 auto-registered
// namespace-0 types) decodes with a nil Value, which looks exactly like
// "no data" instead of "we can't decode this type."
//
// A value that's already JSON-safe (bool, int*, uint*, finite float32/64,
// string, time.Time, []byte, or an *ua.ExtensionObject gopcua successfully
// decoded) passes through unchanged.
func NormalizeReadValue(v interface{}) interface{} {
	switch x := v.(type) {
	case nil:
		return nil
	case float32:
		return normalizeFloat(float64(x))
	case float64:
		return normalizeFloat(x)
	case *ua.NodeID:
		return x.String()
	case *ua.ExpandedNodeID:
		return x.String()
	case *ua.GUID:
		return x.String()
	case *ua.QualifiedName:
		return x.Name
	case *ua.LocalizedText:
		return x.Text
	case ua.StatusCode:
		return x.Error()
	case *ua.ExtensionObject:
		return normalizeExtensionObject(x)
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice && rv.Type() != byteSliceType {
		return normalizeSlice(rv)
	}

	return v
}

// normalizeFloat returns v unchanged if finite, or its string form
// ("NaN"/"+Inf"/"-Inf") otherwise, so JSON marshaling never fails.
func normalizeFloat(v float64) interface{} {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	default:
		return v
	}
}

// normalizeSlice recursively normalizes every element of a typed slice
// (e.g. []int32, []float64, [][]int32) into a []interface{}, since that's
// the only array shape the persistent cache's encoder supports.
func normalizeSlice(rv reflect.Value) []interface{} {
	out := make([]interface{}, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = NormalizeReadValue(rv.Index(i).Interface())
	}
	return out
}

// normalizeExtensionObject passes through a successfully-decoded value
// unchanged, or replaces an undecodable one (Value == nil, meaning gopcua
// has no registered Go type for this DataType's encoding) with a
// descriptive string identifying the type, instead of a misleading null.
func normalizeExtensionObject(e *ua.ExtensionObject) interface{} {
	if e == nil {
		return nil
	}
	if e.Value != nil {
		return e.Value
	}
	return fmt.Sprintf("<undecoded custom DataType: type_id=%s, encoding_mask=%d - this server-specific structure is not registered with this client and its value cannot be decoded>", e.TypeID, e.EncodingMask)
}
