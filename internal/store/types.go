package store

import (
	"encoding/json"
	"time"
)

// ValueEntry is a cached OPC-UA value, keyed by node ID in the values bucket.
type ValueEntry struct {
	Value           interface{} // exact Go type preserved across storage - see encode/decode in value_encoding.go
	Status          string      // ua.StatusCode.String()
	SourceTimestamp time.Time
	ServerTimestamp time.Time
	ReceivedAt      time.Time // when this entry was written; freshness comparisons are the caller's responsibility, not Store's
	Source          string    // "live" | "subscription" - who wrote this entry
}

// TypeInfoEntry is cached node type information, keyed by node ID in the typeinfo bucket.
type TypeInfoEntry struct {
	DataTypeID      uint32
	ValueRank       int32
	ArrayDimensions []uint32
	AccessLevel     byte
	UserAccessLevel byte
	CachedAt        time.Time
}

// BrowseEntry is a cached browse result, keyed by parent node ID in the browse bucket.
type BrowseEntry struct {
	References []BrowseReference
	CachedAt   time.Time
}

// BrowseReference is one child reference within a BrowseEntry.
type BrowseReference struct {
	NodeID         string
	BrowseName     string
	DisplayName    string
	NodeClass      string
	TypeDefinition string
}

// SubscriptionIntent is persisted subscription intent, keyed by subscription
// ID in the subscriptions bucket. It stores intent only (node IDs +
// interval) - never server-assigned ephemeral subscription IDs, which are
// meaningless after a reconnect or restart.
type SubscriptionIntent struct {
	ID         string
	NodeIDs    []string
	IntervalMs int
	CreatedAt  time.Time
}

// valueKind tags the dynamic type of a ValueEntry.Value so it survives a
// JSON round-trip with full fidelity - encoding/json's default interface{}
// unmarshaling collapses every number to float64, which would silently
// change e.g. an int32 value's dynamic type on read-back.
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

// encodedValue is the on-disk representation of a ValueEntry.Value,
// preserving its exact Go type. Raw is used for every scalar kind; Elems is
// used only for kindArray (recursively encoded elements).
type encodedValue struct {
	Kind  valueKind       `json:"kind"`
	Raw   json.RawMessage `json:"raw,omitempty"`
	Elems []encodedValue  `json:"elems,omitempty"`
}

// storedValueEntry is ValueEntry's on-disk shape: identical except Value is
// replaced by its encodedValue representation.
type storedValueEntry struct {
	Value           encodedValue
	Status          string
	SourceTimestamp time.Time
	ServerTimestamp time.Time
	ReceivedAt      time.Time
	Source          string
}
