package store

import (
	"encoding/json"
	"fmt"
	"time"
)

// encode converts a ValueEntry.Value into its tagged on-disk representation,
// preserving its exact Go type across a JSON round-trip. The supported set
// is exactly the Go types internal/opcua/client.go already produces/consumes
// for OPC-UA scalar values, plus []interface{} for arrays. Any other
// dynamic type fails fast rather than silently falling back to lossy
// default JSON encoding.
func encode(v interface{}) (encodedValue, error) {
	switch x := v.(type) {
	case bool:
		return encodeScalar(kindBool, x)
	case int8:
		return encodeScalar(kindInt8, x)
	case int16:
		return encodeScalar(kindInt16, x)
	case int32:
		return encodeScalar(kindInt32, x)
	case int64:
		return encodeScalar(kindInt64, x)
	case uint8:
		return encodeScalar(kindUint8, x)
	case uint16:
		return encodeScalar(kindUint16, x)
	case uint32:
		return encodeScalar(kindUint32, x)
	case uint64:
		return encodeScalar(kindUint64, x)
	case float32:
		return encodeScalar(kindFloat32, x)
	case float64:
		return encodeScalar(kindFloat64, x)
	case string:
		return encodeScalar(kindString, x)
	case time.Time:
		return encodeScalar(kindTime, x)
	case []byte:
		return encodeScalar(kindBytes, x)
	case []interface{}:
		elems := make([]encodedValue, len(x))
		for i, e := range x {
			encoded, err := encode(e)
			if err != nil {
				return encodedValue{}, fmt.Errorf("encode array element %d: %w", i, err)
			}
			elems[i] = encoded
		}
		return encodedValue{Kind: kindArray, Elems: elems}, nil
	default:
		return encodedValue{}, fmt.Errorf("store: cannot encode value of type %T", v)
	}
}

func encodeScalar(kind valueKind, v interface{}) (encodedValue, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return encodedValue{}, fmt.Errorf("store: marshal %s value: %w", kind, err)
	}
	return encodedValue{Kind: kind, Raw: raw}, nil
}

// decode reverses encode, reconstructing the exact original Go type.
func decode(e encodedValue) (interface{}, error) {
	switch e.Kind {
	case kindBool:
		var v bool
		return v, unmarshalScalar(e, &v)
	case kindInt8:
		var v int8
		return v, unmarshalScalar(e, &v)
	case kindInt16:
		var v int16
		return v, unmarshalScalar(e, &v)
	case kindInt32:
		var v int32
		return v, unmarshalScalar(e, &v)
	case kindInt64:
		var v int64
		return v, unmarshalScalar(e, &v)
	case kindUint8:
		var v uint8
		return v, unmarshalScalar(e, &v)
	case kindUint16:
		var v uint16
		return v, unmarshalScalar(e, &v)
	case kindUint32:
		var v uint32
		return v, unmarshalScalar(e, &v)
	case kindUint64:
		var v uint64
		return v, unmarshalScalar(e, &v)
	case kindFloat32:
		var v float32
		return v, unmarshalScalar(e, &v)
	case kindFloat64:
		var v float64
		return v, unmarshalScalar(e, &v)
	case kindString:
		var v string
		return v, unmarshalScalar(e, &v)
	case kindTime:
		var v time.Time
		return v, unmarshalScalar(e, &v)
	case kindBytes:
		var v []byte
		return v, unmarshalScalar(e, &v)
	case kindArray:
		out := make([]interface{}, len(e.Elems))
		for i, elem := range e.Elems {
			decoded, err := decode(elem)
			if err != nil {
				return nil, fmt.Errorf("decode array element %d: %w", i, err)
			}
			out[i] = decoded
		}
		return out, nil
	default:
		return nil, fmt.Errorf("store: unknown encoded value kind %q", e.Kind)
	}
}

// unmarshalScalar decodes e.Raw into out, returning a wrapped error on
// failure. out must be a pointer, matching this function's call sites above.
func unmarshalScalar(e encodedValue, out interface{}) error {
	if err := json.Unmarshal(e.Raw, out); err != nil {
		return fmt.Errorf("store: unmarshal %s value: %w", e.Kind, err)
	}
	return nil
}
