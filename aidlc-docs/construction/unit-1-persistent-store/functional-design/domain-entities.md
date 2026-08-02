# Domain Entities — Unit 1: Persistent Store

## `ValueEntry` (`values` bucket, keyed by node ID)
```go
type ValueEntry struct {
    Value           interface{} // see "Value encoding" below for fidelity handling
    Status          string      // ua.StatusCode.String()
    SourceTimestamp time.Time
    ServerTimestamp time.Time
    ReceivedAt      time.Time   // when this entry was written; freshness comparisons are the caller's job (Q1=A)
    Source          string      // "live" | "subscription" - who wrote this entry; lets CachingClient distinguish a subscription-fed value from an opportunistically-cached live read without depending on SubscriptionManager directly (per application-design/services.md's provenance recommendation)
}
```

## `TypeInfoEntry` (`typeinfo` bucket, keyed by node ID)
```go
type TypeInfoEntry struct {
    DataTypeID      uint32
    ValueRank       int32
    ArrayDimensions []uint32
    AccessLevel     byte
    UserAccessLevel byte
    CachedAt        time.Time
}
```

## `BrowseEntry` (`browse` bucket, keyed by parent node ID)
```go
type BrowseEntry struct {
    References []BrowseReference
    CachedAt   time.Time
}

type BrowseReference struct {
    NodeID         string
    BrowseName     string
    DisplayName    string
    NodeClass      string
    TypeDefinition string
}
```

## `SubscriptionIntent` (`subscriptions` bucket, keyed by subscription ID)
```go
type SubscriptionIntent struct {
    ID         string
    NodeIDs    []string
    IntervalMs int
    CreatedAt  time.Time
}
```

## `encodedValue` (internal, not exported — the answer to Question 2)
`ValueEntry.Value` is `interface{}`, and `encoding/json`'s default
`interface{}` unmarshaling collapses every number to `float64` — an `int32`
value of `42` would read back as `float64(42)`, silently changing its
dynamic type. Per Question 2 (answer B), exact type fidelity is preserved
via a tagged wrapper, stored internally instead of the raw `interface{}`:

```go
type valueKind string

const (
    kindBool    valueKind = "bool"
    kindInt8    valueKind = "int8"
    kindInt16   valueKind = "int16"
    kindInt32   valueKind = "int32"
    kindInt64   valueKind = "int64"
    kindUint8   valueKind = "uint8"
    kindUint16  valueKind = "uint16"
    kindUint32  valueKind = "uint32"
    kindUint64  valueKind = "uint64"
    kindFloat32 valueKind = "float32"
    kindFloat64 valueKind = "float64"
    kindString  valueKind = "string"
    kindTime    valueKind = "time"
    kindBytes   valueKind = "bytes"
    kindArray   valueKind = "array"
)

type encodedValue struct {
    Kind  valueKind       `json:"kind"`
    Raw   json.RawMessage `json:"raw,omitempty"`  // scalar kinds
    Elems []encodedValue  `json:"elems,omitempty"` // kindArray only
}
```

This closed set matches exactly the Go types `internal/opcua/client.go`
already produces/consumes for OPC-UA scalar values (`bool`, `int8`...`int64`,
`uint8`...`uint64`, `float32`/`float64`, `string`, `time.Time`, `[]byte` for
`ByteString`/`Guid`) plus `[]interface{}` for arrays (recursively encoded
element-by-element). See `business-rules.md` for the encode/decode
algorithm and the fail-fast rule for anything outside this set.
