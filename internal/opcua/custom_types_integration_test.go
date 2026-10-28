//go:build integration

// Package opcua's custom-types integration suite verifies this project's
// value-normalization and write-conversion fixes against the real sample
// address space (Boiler, ParkingLot, and the standard Data/Static Scalar/
// Array folders) on the same Microsoft OPC-UA test server image already
// pinned in docker-compose.yml, using the startOPCUATestServer/
// newIntegrationTestClient helpers from subscription_integration_test.go.
// Excluded from the default `go test ./...`/`make test` run via the
// integration build tag - run explicitly with `make test-integration`.
package opcua

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gopcua/opcua/ua"
)

// Sample-server node IDs confirmed live during this fix's development
// (aidlc-docs/audit.md has the full investigation trail):
//   - structureValueNode: Data/Static/Scalar/StructureValue - a Structure-
//     typed node the server itself reports StatusBadDataTypeIDUnknown for,
//     decoding to *ua.ExtensionObject{Value: nil}.
//   - doubleArrayNode: Data/Static/Array/DoubleValue - contains live NaN/
//     +Inf/-Inf values mixed into real data.
//   - int32ArrayNode: Data/Static/Array/Int32Value - writable, used for the
//     array write/read round trip.
//   - guidNode: Data/Static/Scalar/GuidValue - writable, used for the Guid
//     write/read round trip.
//   - primaryVehicleNode: ParkingLot/DriverOfTheMonth/PrimaryVehicle - a
//     custom (non-namespace-0) DataType whose value can desync gopcua's
//     wire decode for the whole batch response it's read in.
//   - lotTypeNode: ParkingLot/LotType - an ordinary, currently-uninitialized
//     sibling of primaryVehicleNode, used to prove a batch read recovers
//     its result even when primaryVehicleNode's read corrupts the batch.
const (
	structureValueNode = "ns=2;i=10239"
	doubleArrayNode    = "ns=2;i=10310"
	int32ArrayNode     = "ns=2;i=10305"
	guidNode           = "ns=2;i=10229"
	primaryVehicleNode = "ns=7;i=376"
	lotTypeNode        = "ns=7;i=380"
)

// TestIntegrationUndecodedExtensionObjectIsNeverRawNil covers root cause 1:
// a custom/vendor Structure DataType gopcua has no registered decoder for
// must never normalize to a bare Go nil (which looks exactly like "no
// data" to a JSON consumer) - it must be a descriptive, non-empty string.
func TestIntegrationUndecodedExtensionObjectIsNeverRawNil(t *testing.T) {
	endpoint := startOPCUATestServer(t)
	client := newIntegrationTestClient(t, endpoint)
	ctx := context.Background()

	results, err := client.Read(ctx, []string{structureValueNode})
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Read() returned %d results, want 1", len(results))
	}

	raw := results[0].Value.Value()
	eo, ok := raw.(*ua.ExtensionObject)
	if !ok || eo.Value != nil {
		t.Skipf("StructureValue no longer decodes to an unregistered *ua.ExtensionObject (got %#v) - this sample server's address space may have changed; nothing to verify here", raw)
	}

	normalized := NormalizeReadValue(raw)
	str, ok := normalized.(string)
	if !ok || str == "" {
		t.Errorf("NormalizeReadValue(undecoded ExtensionObject) = %#v, want a non-empty descriptive string", normalized)
	}

	if _, err := json.Marshal(normalized); err != nil {
		t.Errorf("json.Marshal(normalized undecoded ExtensionObject) error: %v", err)
	}
}

// TestIntegrationNaNInfArrayNormalizesToJSONSafeValue covers root causes 2
// and 3: a real array value containing NaN/Inf must normalize into
// something json.Marshal never rejects, and into the []interface{} shape
// the persistent cache's encoder supports.
func TestIntegrationNaNInfArrayNormalizesToJSONSafeValue(t *testing.T) {
	endpoint := startOPCUATestServer(t)
	client := newIntegrationTestClient(t, endpoint)
	ctx := context.Background()

	results, err := client.Read(ctx, []string{doubleArrayNode})
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if len(results) != 1 || results[0].Status != ua.StatusOK {
		t.Fatalf("Read(%s) = %+v, want a single Good-status result", doubleArrayNode, results)
	}

	normalized := NormalizeReadValue(results[0].Value.Value())
	arr, ok := normalized.([]interface{})
	if !ok {
		t.Fatalf("NormalizeReadValue(DoubleValue array) = %#v (%T), want []interface{}", normalized, normalized)
	}
	if len(arr) == 0 {
		t.Fatal("NormalizeReadValue(DoubleValue array) returned an empty slice, want the sample data")
	}

	if _, err := json.Marshal(normalized); err != nil {
		t.Errorf("json.Marshal(normalized DoubleValue array) error: %v (this is exactly the batch-killing failure root cause 3 fixes)", err)
	}
}

// TestIntegrationBatchReadIsolatesCustomTypeFailure covers the
// batch-corruption finding: a custom-typed node whose value can desync
// gopcua's wire decode for the whole ReadResponse must not take down the
// other, perfectly good nodes' results in the same batch.
func TestIntegrationBatchReadIsolatesCustomTypeFailure(t *testing.T) {
	endpoint := startOPCUATestServer(t)
	client := newIntegrationTestClient(t, endpoint)
	ctx := context.Background()

	results, err := client.Read(ctx, []string{lotTypeNode, primaryVehicleNode, serverCurrentTimeNode})
	if err != nil {
		t.Fatalf("Read() unexpected top-level error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Read() returned %d results, want 3 (one per requested node, even after a batch-level decode failure)", len(results))
	}

	// serverCurrentTimeNode is the standard, continuously-changing node
	// already used by subscription_integration_test.go - it must recover
	// with a real value regardless of primaryVehicleNode's failure.
	if results[2].Status != ua.StatusOK || results[2].Value.Value() == nil {
		t.Errorf("results[2] (%s) = %+v, want a recovered Good-status value", serverCurrentTimeNode, results[2])
	}
}

// TestIntegrationArrayWriteRoundTrips covers the array write wire-format
// fix: writing a []interface{} value used to be silently mistagged as
// TypeIDVariant on the wire (spec-noncompliant bytes) instead of the
// correct array element type - this can only be caught by a real server
// either rejecting the malformed write or echoing back garbage on readback.
func TestIntegrationArrayWriteRoundTrips(t *testing.T) {
	endpoint := startOPCUATestServer(t)
	client := newIntegrationTestClient(t, endpoint)
	ctx := context.Background()

	want := []interface{}{float64(11), float64(22), float64(33)}
	if err := client.Write(ctx, int32ArrayNode, want); err != nil {
		t.Fatalf("Write(%s, array) error: %v", int32ArrayNode, err)
	}

	results, err := client.Read(ctx, []string{int32ArrayNode})
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	got, ok := results[0].Value.Value().([]int32)
	if !ok {
		t.Fatalf("read-back value = %#v (%T), want []int32", results[0].Value.Value(), results[0].Value.Value())
	}
	if len(got) != len(want) {
		t.Fatalf("read-back array length = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != int32(w.(float64)) {
			t.Errorf("read-back array[%d] = %d, want %d", i, got[i], int32(w.(float64)))
		}
	}
}

// TestIntegrationGUIDWriteRoundTrips covers the adjacent Guid wire-tagging
// fix: convertScalarValue used to return a plain string for a Guid-typed
// write, which wire-tags as TypeIDString instead of TypeIDGUID.
func TestIntegrationGUIDWriteRoundTrips(t *testing.T) {
	endpoint := startOPCUATestServer(t)
	client := newIntegrationTestClient(t, endpoint)
	ctx := context.Background()

	const guidStr = "1111AAAA-22BB-33CC-44DD-55EE77FF9900"
	if err := client.Write(ctx, guidNode, guidStr); err != nil {
		t.Fatalf("Write(%s, guid) error: %v", guidNode, err)
	}

	results, err := client.Read(ctx, []string{guidNode})
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	got, ok := results[0].Value.Value().(*ua.GUID)
	if !ok {
		t.Fatalf("read-back value = %#v (%T), want *ua.GUID", results[0].Value.Value(), results[0].Value.Value())
	}
	if got.String() != guidStr {
		t.Errorf("read-back Guid = %s, want %s", got.String(), guidStr)
	}
}

// TestIntegrationWriteRejectsCustomNamespaceDataType covers the namespace-
// blindness fix end to end against a real custom-namespace node.
func TestIntegrationWriteRejectsCustomNamespaceDataType(t *testing.T) {
	endpoint := startOPCUATestServer(t)
	client := newIntegrationTestClient(t, endpoint)
	ctx := context.Background()

	err := client.Write(ctx, primaryVehicleNode, map[string]interface{}{"foo": "bar"})
	if err == nil {
		t.Error("Write() to a custom-namespace DataType expected an error, got nil")
	}
}
