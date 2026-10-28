package opcua

import (
	"math"
	"reflect"
	"testing"

	"github.com/gopcua/opcua/ua"
)

func TestNormalizeReadValuePassthrough(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
	}{
		{"bool", true},
		{"int32", int32(42)},
		{"float64 finite", 3.14},
		{"string", "hello"},
		{"nil", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeReadValue(tt.in)
			if !reflect.DeepEqual(got, tt.in) {
				t.Errorf("NormalizeReadValue(%#v) = %#v, want unchanged", tt.in, got)
			}
		})
	}
}

func TestNormalizeReadValueBytesPassthrough(t *testing.T) {
	in := []byte{1, 2, 3}
	got := NormalizeReadValue(in)
	gotBytes, ok := got.([]byte)
	if !ok {
		t.Fatalf("NormalizeReadValue([]byte) = %#v (%T), want []byte", got, got)
	}
	if !reflect.DeepEqual(gotBytes, in) {
		t.Errorf("NormalizeReadValue([]byte) = %#v, want %#v", gotBytes, in)
	}
}

func TestNormalizeReadValueTypedSlices(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want []interface{}
	}{
		{"[]int32", []int32{1, 2, 3}, []interface{}{int32(1), int32(2), int32(3)}},
		{"[]float64", []float64{1.5, 2.5}, []interface{}{1.5, 2.5}},
		{"[]bool", []bool{true, false}, []interface{}{true, false}},
		{"[]string", []string{"a", "b"}, []interface{}{"a", "b"}},
		{"empty []int32", []int32{}, []interface{}{}},
		{"nil []int32", []int32(nil), []interface{}{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeReadValue(tt.in)
			gotSlice, ok := got.([]interface{})
			if !ok {
				t.Fatalf("NormalizeReadValue(%#v) = %#v (%T), want []interface{}", tt.in, got, got)
			}
			if !reflect.DeepEqual(gotSlice, tt.want) {
				t.Errorf("NormalizeReadValue(%#v) = %#v, want %#v", tt.in, gotSlice, tt.want)
			}
		})
	}
}

func TestNormalizeReadValueNestedSlices(t *testing.T) {
	in := [][]int32{{1, 2}, {3, 4}}
	got := NormalizeReadValue(in)
	want := []interface{}{
		[]interface{}{int32(1), int32(2)},
		[]interface{}{int32(3), int32(4)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NormalizeReadValue(%#v) = %#v, want %#v", in, got, want)
	}
}

func TestNormalizeReadValueSliceWithNaN(t *testing.T) {
	in := []float64{1.0, math.NaN(), math.Inf(1), math.Inf(-1)}
	got := NormalizeReadValue(in)
	want := []interface{}{1.0, "NaN", "+Inf", "-Inf"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NormalizeReadValue(%#v) = %#v, want %#v", in, got, want)
	}
}

func TestNormalizeReadValueNodeID(t *testing.T) {
	in := ua.NewNumericNodeID(2, 42)
	got := NormalizeReadValue(in)
	want := in.String()
	if got != want {
		t.Errorf("NormalizeReadValue(NodeID) = %#v, want %q", got, want)
	}
}

func TestNormalizeReadValueExpandedNodeID(t *testing.T) {
	in := ua.NewNumericExpandedNodeID(2, 42)
	got := NormalizeReadValue(in)
	want := in.String()
	if got != want {
		t.Errorf("NormalizeReadValue(ExpandedNodeID) = %#v, want %q", got, want)
	}
}

func TestNormalizeReadValueGUID(t *testing.T) {
	in := ua.NewGUID("72962B91-FA75-4AE6-8D28-B404DC7DAF63")
	got := NormalizeReadValue(in)
	want := in.String()
	if got != want {
		t.Errorf("NormalizeReadValue(GUID) = %#v, want %q", got, want)
	}
}

func TestNormalizeReadValueQualifiedName(t *testing.T) {
	in := &ua.QualifiedName{NamespaceIndex: 2, Name: "MyNode"}
	got := NormalizeReadValue(in)
	if got != "MyNode" {
		t.Errorf("NormalizeReadValue(QualifiedName) = %#v, want %q", got, "MyNode")
	}
}

func TestNormalizeReadValueLocalizedText(t *testing.T) {
	in := ua.NewLocalizedTextWithLocale("Hello", "en-US")
	got := NormalizeReadValue(in)
	if got != "Hello" {
		t.Errorf("NormalizeReadValue(LocalizedText) = %#v, want %q", got, "Hello")
	}
}

func TestNormalizeReadValueStatusCode(t *testing.T) {
	got := NormalizeReadValue(ua.StatusBadNodeIDUnknown)
	want := ua.StatusBadNodeIDUnknown.Error()
	if got != want {
		t.Errorf("NormalizeReadValue(StatusCode) = %#v, want %q", got, want)
	}
}

func TestNormalizeReadValueExtensionObjectDecoded(t *testing.T) {
	in := &ua.ExtensionObject{Value: &ua.BuildInfo{ProductName: "test-product"}}
	got := NormalizeReadValue(in)
	bi, ok := got.(*ua.BuildInfo)
	if !ok || bi.ProductName != "test-product" {
		t.Errorf("NormalizeReadValue(decoded ExtensionObject) = %#v, want passthrough *ua.BuildInfo", got)
	}
}

func TestNormalizeReadValueExtensionObjectUndecoded(t *testing.T) {
	in := &ua.ExtensionObject{
		EncodingMask: ua.ExtensionObjectBinary,
		TypeID:       ua.NewNumericExpandedNodeID(6, 314),
		Value:        nil,
	}
	got := NormalizeReadValue(in)
	str, ok := got.(string)
	if !ok {
		t.Fatalf("NormalizeReadValue(undecoded ExtensionObject) = %#v (%T), want a string", got, got)
	}
	if str == "" {
		t.Error("NormalizeReadValue(undecoded ExtensionObject) returned an empty string, want a descriptive message")
	}
}

func TestNormalizeReadValueScalarNaNInf(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want string
	}{
		{"NaN float64", math.NaN(), "NaN"},
		{"+Inf float64", math.Inf(1), "+Inf"},
		{"-Inf float64", math.Inf(-1), "-Inf"},
		{"NaN float32", float32(math.NaN()), "NaN"},
		{"+Inf float32", float32(math.Inf(1)), "+Inf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeReadValue(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeReadValue(%v) = %#v, want %q", tt.in, got, tt.want)
			}
		})
	}
}
