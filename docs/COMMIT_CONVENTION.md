# Commit message convention

Every commit message starts with `type: ` followed by a short, imperative summary
(what the commit does, not what it fixes-in-the-abstract). Keep the summary line
under ~72 characters; add a body only when the *why* isn't obvious from the diff
(e.g. linking back to a `docs/plan/plan.md` item ID or a `findings.md` finding ID).

## Types

- `feat` - new capability or behavior that didn't exist before.
- `fix` - corrects incorrect behavior (bug fix), including data races,
  correctness bugs, and validation gaps.
- `chore` - dependency bumps, tooling, config plumbing with no behavior change.
- `docs` - documentation only (README, plan docs, comments-as-docs).
- `refactor` - internal restructuring with no observable behavior change.
- `test` - adds or fixes tests only.
- `perf` - performance improvement with no behavior change.
- `build` - build system, Makefile, Dockerfile changes.
- `ai` - AI-authored scaffolding/process artifacts (e.g. this repo's aidlc rules),
  not application code changes.

## Examples

```
fix: propagate ValidateValueForNode failures from Write()

feat: implement RWMutex for Client to guard client/connected fields

fix: handle potential deadlock in Connect() retry loop

chore: bump caarlos0/env to v11.4.1 (drop retracted version)

test: add concurrent-access race test for opcua.Client

docs: document commit message convention
```

## Referencing the plan

When a commit implements a specific `docs/plan/plan.md` item, mention its ID
(e.g. `P0-1`, `P1-3`) in the commit body so the audit trail in `findings.md` /
`plan.md` stays traceable to the actual change:

```
fix: correct GetNodeClass int32 wire-type assertion

Implements P0-3. NodeClass decodes as int32 on the wire (gopcua
ua/variant.go), not ua.NodeClass - the old assertion always failed
against a real server.
```
