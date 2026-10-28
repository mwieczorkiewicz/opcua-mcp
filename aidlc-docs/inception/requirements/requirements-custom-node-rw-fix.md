# Requirements - Custom-Node Read/Write Correctness Bugfix Pass

## Intent Analysis Summary

- **User Request**: Reading "custom nodes" (specifically Boiler and ParkingLot sample objects on the Microsoft OPC-UA test server this repo already pins) sometimes returns null values or triggers "cannot encode" cache errors, while sibling nodes read fine. Diagnose via comprehensive code analysis and live testing against the real test server, then fix.
- **Request Type**: Bug fix / correctness hardening, no new MCP tools or user-facing features.
- **Initial Scope Estimate**: Multiple existing files (`internal/opcua/client.go`, `caching_client.go`, `subscription.go`, `internal/store/types.go`, `internal/mcp/server.go`) - no new components.
- **Initial Complexity Estimate**: Moderate-to-complex. Four confirmed root causes, all downstream of the same gap (value conversion only handles a fixed builtin-scalar set), plus one newly-discovered severe bug (array writes produce spec-noncompliant wire bytes) found while tracing the write path.

## Requirements-gathering method

Requirements were gathered interactively via `AskUserQuestion` during the planning phase (not a written question file, given the narrower bugfix scope compared to a new-feature Phase). User answers, verbatim intent:
1. Observed symptoms: null/empty values on read, "cannot encode value of type ..." errors, mostly on reads.
2. Test approach: use `docker-compose`'s pinned Microsoft OPC-UA test server for live testing/debugging, not mocks alone.
3. Scope: fix all four identified issues (ExtensionObject data loss, array/typed-value caching failures, batch-killing NaN/Inf marshal errors, namespace-blind write validation), not a narrower subset.
4. Mid-turn hint: symptoms were seen specifically on the Boiler and ParkingLot sample objects - both are OPC Foundation reference-server sample objects with custom/vendor DataTypes in non-zero namespaces, consistent with the ExtensionObject/namespace-blindness root causes.

## Functional Requirements

- **FR-1**: A node whose DataType is an unregistered custom/vendor Structure (ExtensionObject) must never read back as a bare JSON `null` when its status is Good. It must show a real decoded value if the type happens to be one of gopcua's ~308 auto-registered namespace-0 types, or an honest "undecoded custom type" message identifying the type ID otherwise. Full generic decoding of arbitrary vendor DataTypeDictionaries is explicitly out of scope (would require dynamically fetching and parsing the server's `Opc.Ua.BinarySchema`, which gopcua provides no tooling for).
- **FR-2**: Array-typed values (and NodeID/GUID/QualifiedName/LocalizedText/StatusCode-typed values) must be cacheable by the persistent store, matching what live reads already return, so `opcua_read`'s caching and subscription-push paths work for these types the same as for plain scalars.
- **FR-3**: A single bad value (NaN/Inf float, or any other value that would fail to JSON-marshal) in a multi-node `opcua_read` batch must never cause the entire batch response to fail; other nodes' results must still be returned.
- **FR-4**: `opcua_write` must reject writes to nodes with a custom (non-namespace-0) DataType with a clear, actionable error, instead of either silently misclassifying them as a same-numbered builtin type or failing with a low-level, confusing `ua.NewVariant` error.
- **FR-5**: `opcua_write` of an array-typed value must produce spec-compliant OPC-UA wire bytes (the array's declared element TypeID on the wire must match the bytes actually written for each element) - this generalizes beyond custom types to every array write, since the current implementation is broken for all of them.

## Non-Functional Requirements

- **Correctness over feature completeness**: prefer an honest, clearly-labeled "can't decode this" result over a silently-wrong or silently-dropped one, consistent with this codebase's existing partial-batch-read philosophy (`client.go`'s documented "one bad node must not discard the good results in the same batch" rule).
- **No regressions**: existing scalar read/write/cache behavior, all 18 registered MCP tools, and all currently-passing tests must remain green (`go build/vet/test -race`, `gofmt`, `golint`, per CLAUDE.md hard rules).
- **No new dependencies, tool names, env vars, or telemetry payload fields** - confirmed unnecessary by the design (fix uses only `reflect`, already-imported gopcua types, and existing store/cache primitives).
- **Backward-compatible cache schema change**: adding `DataTypeNamespace` to `store.TypeInfoEntry` must not require a migration or break decoding of previously-cached entries (JSON zero-value default of namespace 0 matches today's implicit behavior).

## Out of Scope

- Generic decoding of arbitrary vendor/custom ExtensionObject structures (would need DataTypeDictionary support).
- Writing custom/complex (non-namespace-0) DataTypes - only validated-and-rejected with a clear error, not supported.
- The five MCP tool handlers that read via the raw `*Client` instead of `CachingClient` (`handleNodeInfo`, `handleServerInfoFallback`, `handleGetValue`, `handleGetValueByName`, the node-URI resource handler) gain the JSON-marshal-safety fix but remain permanently uncached - that's a separate, pre-existing architectural gap, not addressed here.
- True multi-dimensional array validation against all `ArrayDimensions` entries (today only `ArrayDimensions[0]` is checked) - not touched unless live testing surfaces it as active on the sample server.

## Approval

Approved via plan-mode `AskUserQuestion` answers and explicit instruction to proceed in autonomous mode following this repo's AIDLC workflow, using sane defaults and self-resolving judgment calls without pausing for approval gates, escalating only for genuine blockers. See `aidlc-docs/audit.md` for the verbatim exchange.
