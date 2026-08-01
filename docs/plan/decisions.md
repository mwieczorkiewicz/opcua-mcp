# Open Decisions — opcua-mcp Improvement Plan

Decisions requiring human sign-off before the corresponding plan item(s) execute. Three top-level scope decisions were already made with the user during planning and are recorded first for context; the rest are still open.

---

## Already decided (recorded for context)

**D0. H1 fix approach** — *Decided: force stderr whenever `SERVER_TRANSPORT=stdio`, regardless of `SERVER_LOG_OUTPUT`, with a warning if the user explicitly set `stdout`.* Implements as P0-2. Rejected alternatives: changing only the default value (leaves an explicit override footgun live), or documentation-only (leaves the critical bug live by default).

**D1. H11 tool-surface cleanup approach** — *Decided: remove `opcua_get_value` and `opcua_browse` outright (breaking change); gate `opcua_debug_search`/`opcua_ensure_server_nodes` behind opt-in `MCP_ENABLE_DEBUG_TOOLS` (default false).* Implements as P3-1/P3-2/P3-3. Rejected alternatives: deprecate-in-place with no removal (avoids breaking change but leaves tool sprawl permanently), or leave untouched (defers the whole issue).

**D2. Dependency upgrade scope** — *Decided: only fix the retracted `caarlos0/env` version (→ v11.4.1) in this plan.* Implements as P0-5. `gopcua v0.8.0→v0.9.0` and `mcp-go v0.39.1→v0.57.0` are explicitly deferred — see D3/D4 below, which formalize that deferral as its own decision.

---

## Open — needs sign-off

### D3. `gopcua` upgrade (v0.8.0 → v0.9.0)
- **Context**: one minor version behind. Not evaluated for breaking changes in this session.
- **Options**:
  1. **Defer indefinitely** — stay on v0.8.0 until a specific need arises (e.g., a bug fix or feature in v0.9.0 that's actually wanted).
  2. **Schedule a dedicated investigation** — a follow-up session reads the v0.9.0 changelog/diff against v0.8.0, checks for breaking changes against every gopcua API this codebase uses (`Read`, `Write`, `Browse`, `BrowseNext`, `Subscribe`, `Monitor`, `State`, `Connect`), and either upgrades or documents why not.
- **Recommendation**: Option 2, scheduled *after* Phase 2 lands (F1/F2 land against the currently-verified v0.8.0 API; upgrading mid-implementation risks invalidating the verified API assumptions this plan is built on).

### D4. `mcp-go` upgrade (v0.39.1 → v0.57.0)
- **Context**: an 18-minor-version gap — very likely to include breaking changes to tool registration, transport handling, or the stdio/http server APIs this codebase depends on directly.
- **Options**:
  1. **Defer indefinitely.**
  2. **Schedule a dedicated investigation**, same shape as D3 but higher-risk given the version gap size — should happen in isolation (its own branch/session), not bundled with Phase 2/3 feature work, since a breaking mcp-go change could touch every tool handler.
- **Recommendation**: Option 2, but explicitly *after* Phase 3 (tool-surface cleanup) lands — upgrading mcp-go before the tool removals/additions settle means re-testing the migration twice.

### D5. `bbolt` upgrade (v1.4.0 → v1.5.0)
- **Context**: low-risk minor bump, surfaced while promoting bbolt to a direct dependency in P2-1.
- **Options**:
  1. **Bundle with P2-1** — bump to v1.5.0 while promoting to direct, since it's already being touched.
  2. **Defer** — promote v1.4.0 to direct as-is, upgrade separately later.
- **Recommendation**: Option 1 (bundle) — low risk, avoids a second churn on the same dependency shortly after.

### D6. `OPCUA_SERVER_CERT` — implement or remove
- **Context**: this config field is fully documented in the README (implying server-certificate pinning/verification) but is never read anywhere in the code — a functional/documentation mismatch (H9). P3-4 is gated on this decision.
- **Options**:
  1. **Implement it** — wire `ServerCert` into the TLS/secure-channel setup in `Connect()`/`addCertificateAuth` to actually pin/verify the server's certificate. Real security value if this deployment talks to OPC-UA servers over an untrusted network, but adds scope to Phase 3 and needs testing against a server presenting both a matching and mismatched certificate.
  2. **Remove it** — delete the dead field and its README documentation, since it currently does nothing and no one may be relying on it (though anyone who *thinks* they're using it today is currently silently unprotected — worth flagging as a latent security-documentation risk regardless of which option is chosen).
- **Recommendation**: Option 2 (remove) as the low-risk default for this plan, *unless* the operator's deployment actually talks to OPC-UA servers over a network where server-certificate pinning matters — if so, Option 1 is the right call but should be scoped as its own dedicated plan item outside this pass, not folded into P3-4's cleanup.

### D7. Docker non-root UID
- **Context**: P3-7 needs a specific numeric UID for the `scratch`-based final image (no `/etc/passwd` available in `scratch`, so a numeric UID is required rather than a named user).
- **Options**:
  1. **65532** (`nonroot`, the same convention Google's `distroless` and many Chainguard images use) — a widely recognized "well-known nonroot" convention, likely to be least surprising in an already-Chainguard-based Dockerfile.
  2. **10001** or another operator-chosen value — if there's an existing internal convention for this deployment/fleet.
- **Recommendation**: Option 1 (65532) unless the operator has an existing internal UID convention for containerized services.

### D8. `golangci-lint`/`staticcheck` unavailability
- **Context**: neither tool was usable in this development environment during the audit (`golangci-lint`: not installed; `staticcheck`: failed with a Go 1.26.5 toolchain/export-format incompatibility — likely a staticcheck-version-vs-Go-version mismatch, not a code issue). This means local static-analysis coverage is currently zero beyond `go vet` (added in P3-6) and `gofmt`.
- **Options**:
  1. **Wire both into CI only** (e.g. GitHub Actions with pinned tool versions compatible with the repo's Go version), not relied upon locally.
  2. **Fix local tooling** — install/update `golangci-lint` and a `staticcheck` version compatible with Go 1.26.5 in the dev environment.
- **Recommendation**: Option 1 — CI with pinned versions is more reproducible than chasing local toolchain compatibility, and the Makefile's `lint`/`security-scan` targets already silently no-op when tools are missing (itself a finding worth fixing regardless of which option is chosen — a CI-only lint pass should hard-fail if the tool isn't present in that environment, unlike the current local-graceful-degradation behavior which is fine for local dev but wrong for CI).

### D9. `MCP_VERSION` bump alongside Phase 3's breaking tool removals
- **Context**: `MCPConfig.Version` (`MCP_VERSION`, currently `"1.0.0"`) is reported to MCP clients. Phase 3 removes two tools and changes default tool visibility — a breaking change for existing integrations.
- **Options**:
  1. **Bump to `2.0.0`** alongside P3-1/P3-2/P3-3 landing, signaling the breaking change through the version an MCP client can observe.
  2. **Leave at `1.0.0`** — rely on the README changelog/release notes as the sole signal.
- **Recommendation**: Option 1 — costs nothing and gives MCP clients a machine-observable signal, not just a documentation one.

### D10. Read-through cache staleness vs. write-invalidation
- **Context**: not explicitly designed in F1/F2 — after `opcua_write` succeeds, should the `values`-bucket cache entry for that node be invalidated/updated immediately, or left to expire naturally per `max_age_ms`/subscription push? If a node is subscribed, the next push will correct it quickly; if it's only read-through cached (not subscribed), a stale pre-write value could be served for up to `max_age_ms` after a successful write.
- **Options**:
  1. **Invalidate the cache entry for the written node on a successful `Write()`** — cheap (one `store.Delete` call), closes the staleness window entirely for the write-then-read-back pattern, which is a plausible common usage pattern for an LLM-driven tool.
  2. **Leave it** — accept the staleness window, document it in the `opcua_read` tool description (`max_age_ms` semantics already imply "may be stale by up to this much").
- **Recommendation**: Option 1 — small, cheap addition to P2-6/Write() that meaningfully improves correctness for a realistic usage pattern; should be folded into P2-6's acceptance criteria if approved (flagging here rather than silently expanding P2-6's scope without sign-off).
