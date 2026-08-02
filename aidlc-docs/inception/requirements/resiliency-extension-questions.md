# Resiliency Extension — Mandatory Clarifying Questions

You opted into the resiliency baseline extension. Its rules mandate asking
the following questions directly (not deciding on your behalf), even though
several read as cloud-deployment-oriented and may not map cleanly onto a
single Go binary/container with no hosted deployment target in this repo. If
a question doesn't apply, its N/A-style option is usually listed — pick that
rather than skipping the question.

## Question 1 — RTO/RPO Goals and Disaster Recovery Strategy
What are your Recovery Time Objective (RTO) and Recovery Point Objective (RPO) goals for the new persistent cache (bbolt file)? This drives the DR strategy for that data.

A) RPO/RTO: Hours — Backup & Restore. Lowest cost. Suitable for non-critical workloads.

B) RPO/RTO: 10s of minutes — Pilot Light.

C) RPO/RTO: Minutes — Warm Standby.

D) RPO/RTO: Near real-time — Multi-site Active/Active.

E) N/A — this is a local cache/derived data (values are re-readable from the live OPC-UA server; subscription intent can be manually re-created); no formal RTO/RPO or cross-region DR is needed.

X) Other (please describe after [Answer]: tag below)

[Answer]: E)

## Question 2 — Change Management Process
How should production changes for this workload be governed?

A) Use our existing organizational change management process — I'll name it after [Answer]: below.

B) No formal process exists yet — propose a lightweight one (change record + approval + rollback note).

C) N/A — this workload is exempt from formal change management (e.g., internal/self-hosted tooling, not a managed production service).

X) Other (describe after [Answer]: tag below)

[Answer]: C)

## Question 3 — CI/CD and Deployment Tooling
What CI/CD tooling and deployment process should this workload use?

A) Use our existing CI/CD pipeline — I'll name it after [Answer]: below.

B) No pipeline exists — propose one appropriate to a Go binary + Docker image.

C) N/A — no CI/CD pipeline in scope for this repo/pass; deployment is manual (operator builds/runs the binary or container themselves).

X) Other (describe after [Answer]: tag below)

[Answer]: A)

## Question 4 — Rollback Mechanism
How should a failed deployment be rolled back?

A) Redeploy previous binary/image version (version-pinned rollback)

B) Blue/green swap back to the previous environment

C) Canary auto-rollback on health/metric regression

D) Database-aware rollback required (the new bbolt file's schema could need a migration-reversal story) — flag for explicit design

E) N/A — no formal rollback mechanism needed at this stage (single-operator-deployed binary)

X) Other (describe after [Answer]: tag below)

[Answer]: E)

## Question 5 — Deployment Style
What deployment strategy is acceptable for this workload's risk profile?

A) Direct / in-place (lowest cost, highest blast radius)

B) Rolling (gradual instance replacement)

C) Blue/green (zero-downtime cutover)

D) Canary (progressive traffic shift with automated rollback)

E) N/A — single instance, operator-managed restarts; no formal deployment strategy needed

X) Other (describe after [Answer]: tag below)

[Answer]:X) Experimental, this will be used probably most in laboratory setting.

## Question 6 — Regional Topology
Does this workload require multi-region deployment, or is single-region sufficient?

A) Single-region, multi-zone

B) Multi-region active-passive

C) Multi-region active-active

D) N/A — no cloud regions involved; this runs as a single process/container wherever the operator deploys it

X) Other (describe after [Answer]: tag below)

[Answer]: X)

## Question 7 — Incident Response Process
How are production incidents handled for this workload?

A) Use our existing incident response process — I'll name it after [Answer]: below.

B) No formal process exists — propose a lightweight incident response/Correction-of-Errors process.

C) N/A — no formal incident response process for this project at this stage.

X) Other (describe after [Answer]: tag below)

[Answer]: C)
