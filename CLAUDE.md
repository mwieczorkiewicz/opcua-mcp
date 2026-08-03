# PRIORITY: This workflow OVERRIDES all other built-in workflows
# When user requests software development, ALWAYS follow this workflow FIRST

## Adaptive Workflow Principle
**The workflow adapts to the work, not the other way around.**

The AI model intelligently assesses what stages are needed based on:
1. User's stated intent and clarity
2. Existing codebase state (if any)
3. Complexity and scope of change
4. Risk and impact assessment

## MANDATORY: Rule Details Loading
**CRITICAL**: When performing any phase, you MUST read and use relevant content from rule detail files. Check these paths in order and use the first one that exists, regardless of which IDE or setup method was used:
- `.aidlc/aidlc-rules/aws-aidlc-rule-details/` (typical with AI-assisted setup)
- `.aidlc-rule-details/` (typical with Cursor, Cline, Claude Code, GitHub Copilot, OpenAI Codex)
- `.kiro/aws-aidlc-rule-details/` (typical with Kiro IDE and CLI)
- `.amazonq/aws-aidlc-rule-details/` (typical with Amazon Q Developer)

All subsequent rule detail file references (e.g., `common/process-overview.md`, `inception/workspace-detection.md`) are relative to whichever rule details directory was resolved above.

**Common Rules**: ALWAYS load common rules at workflow start:
- Load `common/process-overview.md` for workflow overview
- Load `common/session-continuity.md` for session resumption guidance
- Load `common/content-validation.md` for content validation requirements
- Load `common/question-format-guide.md` for question formatting rules
- Reference these throughout the workflow execution

## MANDATORY: Extensions Loading (Context-Optimized)
**CRITICAL**: At workflow start, scan the `extensions/` directory recursively but load ONLY lightweight opt-in files - NOT full rule files. Full rule files are loaded on-demand after the user opts in.

**Loading process**:
1. List all subdirectories under `extensions/` (e.g., `extensions/security/`, `extensions/compliance/`)
2. In each subdirectory, load ONLY `*.opt-in.md` files - these contain the extension's opt-in prompt. The corresponding rules file is derived by convention: strip the `.opt-in.md` suffix and append `.md` (e.g., `security-baseline.opt-in.md` → `security-baseline.md`)
3. Do NOT load full rule files (e.g., `security-baseline.md`) at this stage

**Deferred Rule Loading**:
- During Requirements Analysis, opt-in prompts from the loaded `*.opt-in.md` files are presented to the user
- When the user opts IN for an extension, load the corresponding rules file (derived by naming convention) at that point
- When the user opts OUT, the full rules file is never loaded - saving context
- Extensions without a matching `*.opt-in.md` file are always enforced - load their rule files immediately at workflow start

**Enforcement** (applies only to loaded/enabled extensions):
- Extension rules are hard constraints, not optional guidance
- At each stage, the model intelligently evaluates which extension rules are applicable based on the stage's purpose, the artifacts being produced, and the context of the work - enforce only those rules that are relevant
- Rules that are not applicable to the current stage should be marked as N/A in the compliance summary (this is not a blocking finding)
- Non-compliance with any applicable enabled extension rule is a **blocking finding** - do NOT present stage completion until resolved
- When presenting stage completion, include a summary of extension rule compliance (compliant/non-compliant/N/A per rule, with brief rationale for N/A determinations)

**Conditional Enforcement**: Extensions may be conditionally enabled/disabled. See `inception/requirements-analysis.md` for the opt-in mechanism. Before enforcing any extension at ANY stage, check its `Enabled` status in `aidlc-docs/aidlc-state.md` under `## Extension Configuration`. Skip disabled extensions and log the skip in audit.md. Default to enforced if no configuration exists. 

## MANDATORY: Content Validation
**CRITICAL**: Before creating ANY file, you MUST validate content according to `common/content-validation.md` rules:
- Validate Mermaid diagram syntax
- Validate ASCII art diagrams (see `common/ascii-diagram-standards.md`)
- Escape special characters properly
- Provide text alternatives for complex visual content
- Test content parsing compatibility

## MANDATORY: Question File Format
**CRITICAL**: When asking questions at any phase, you MUST follow question format guidelines.

**See `common/question-format-guide.md` for complete question formatting rules including**:
- Multiple choice format (A, B, C, D, E options)
- [Answer]: tag usage
- Answer validation and ambiguity resolution

## MANDATORY: Custom Welcome Message
**CRITICAL**: When starting ANY software development request, you MUST display the welcome message.

**How to Display Welcome Message**:
1. Load the welcome message from `common/welcome-message.md` (in the resolved rule details directory)
2. Display the complete message to the user
3. This should only be done ONCE at the start of a new workflow
4. Do NOT load this file in subsequent interactions to save context space

# Adaptive Software Development Workflow

---

# INCEPTION PHASE

**Purpose**: Planning, requirements gathering, and architectural decisions

**Focus**: Determine WHAT to build and WHY

**Stages in INCEPTION PHASE**:
- Workspace Detection (ALWAYS)
- Reverse Engineering (CONDITIONAL - Brownfield only)
- Requirements Analysis (ALWAYS - Adaptive depth)
- User Stories (CONDITIONAL)
- Workflow Planning (ALWAYS)
- Application Design (CONDITIONAL)
- Units Generation (CONDITIONAL)

---

## Workspace Detection (ALWAYS EXECUTE)

1. **MANDATORY**: Log initial user request in audit.md with complete raw input
2. Load all steps from `inception/workspace-detection.md`
3. Execute workspace detection:
   - Check for existing aidlc-state.md (resume if found)
   - Scan workspace for existing code
   - Determine if brownfield or greenfield
   - Check for existing reverse engineering artifacts
4. Determine next phase: Reverse Engineering (if brownfield and no artifacts) OR Requirements Analysis
5. **MANDATORY**: Log findings in audit.md
6. Present completion message to user (see workspace-detection.md for message formats)
7. Automatically proceed to next phase

## Reverse Engineering (CONDITIONAL - Brownfield Only)

**Execute IF**:
- Existing codebase detected
- No previous reverse engineering artifacts found

**Skip IF**:
- Greenfield project
- Previous reverse engineering artifacts exist

**Execution**:
1. **MANDATORY**: Log start of reverse engineering in audit.md
2. Load all steps from `inception/reverse-engineering.md`
3. Execute reverse engineering:
   - Analyze all packages and components
   - Generate a business overview of the whole system covering the business transactions
   - Generate architecture documentation
   - Generate code structure documentation
   - Generate API documentation
   - Generate component inventory
   - Generate Interaction Diagrams depicting how business transactions are implemented across components
   - Generate technology stack documentation
   - Generate dependencies documentation

4. **Wait for Explicit Approval**: Present detailed completion message (see reverse-engineering.md for message format) - DO NOT PROCEED until user confirms
5. **MANDATORY**: Log user's response in audit.md with complete raw input

## Requirements Analysis (ALWAYS EXECUTE - Adaptive Depth)

**Always executes** but depth varies based on request clarity and complexity:
- **Minimal**: Simple, clear request - just document intent analysis
- **Standard**: Normal complexity - gather functional and non-functional requirements
- **Comprehensive**: Complex, high-risk - detailed requirements with traceability

**Execution**:
1. **MANDATORY**: Log any user input during this phase in audit.md
2. Load all steps from `inception/requirements-analysis.md`
3. Execute requirements analysis:
   - Load reverse engineering artifacts (if brownfield)
   - Analyze user request (intent analysis)
   - Determine requirements depth needed
   - Assess current requirements
   - Ask clarifying questions (if needed)
   - Generate requirements document
4. Execute at appropriate depth (minimal/standard/comprehensive)
5. **Wait for Explicit Approval**: Follow approval format from requirements-analysis.md detailed steps - DO NOT PROCEED until user confirms
6. **MANDATORY**: Log user's response in audit.md with complete raw input

## User Stories (CONDITIONAL)

**INTELLIGENT ASSESSMENT**: Use multi-factor analysis to determine if user stories add value:

**ALWAYS Execute IF** (High Priority Indicators):
- New user-facing features or functionality
- Changes affecting user workflows or interactions
- Multiple user types or personas involved
- Complex business requirements with acceptance criteria needs
- Cross-functional team collaboration required
- Customer-facing API or service changes
- New product capabilities or enhancements

**LIKELY Execute IF** (Medium Priority - Assess Complexity):
- Modifications to existing user-facing features
- Backend changes that indirectly affect user experience
- Integration work that impacts user workflows
- Performance improvements with user-visible benefits
- Security enhancements affecting user interactions
- Data model changes affecting user data or reports

**COMPLEXITY-BASED ASSESSMENT**: For medium priority cases, execute user stories if:
- Request involves multiple components or services
- Changes span multiple user touchpoints
- Business logic is complex or has multiple scenarios
- Requirements have ambiguity that stories could clarify
- Implementation affects multiple user journeys
- Change has significant business impact or risk

**SKIP ONLY IF** (Low Priority - Simple Cases):
- Pure internal refactoring with zero user impact
- Simple bug fixes with clear, isolated scope
- Infrastructure changes with no user-facing effects
- Technical debt cleanup with no functional changes
- Developer tooling or build process improvements
- Documentation-only updates

**ASSESSMENT CRITERIA**: When in doubt, favor inclusion of user stories for:
- Requests with business stakeholder involvement
- Changes requiring user acceptance testing
- Features with multiple implementation approaches
- Work that benefits from shared team understanding
- Projects where requirements clarity is valuable

**ASSESSMENT PROCESS**: 
1. Analyze request complexity and scope
2. Identify user impact (direct or indirect)
3. Evaluate business context and stakeholder needs
4. Consider team collaboration benefits
5. Default to inclusion for borderline cases

**Note**: If Requirements Analysis executed, Stories can reference and build upon those requirements.

**User Stories has two parts within one stage**:
1. **Part 1 - Planning**: Create story plan with questions, collect answers, analyze for ambiguities, get approval
2. **Part 2 - Generation**: Execute approved plan to generate stories and personas

**Execution**:
1. **MANDATORY**: Log any user input during this phase in audit.md
2. Load all steps from `inception/user-stories.md`
3. **MANDATORY**: Perform intelligent assessment (Step 1 in user-stories.md) to validate user stories are needed
4. Load reverse engineering artifacts (if brownfield)
5. If Requirements exist, reference them when creating stories
6. Execute at appropriate depth (minimal/standard/comprehensive)
7. **PART 1 - Planning**: Create story plan with questions, wait for user answers, analyze for ambiguities, get approval
8. **PART 2 - Generation**: Execute approved plan to generate stories and personas
9. **Wait for Explicit Approval**: Follow approval format from user-stories.md detailed steps - DO NOT PROCEED until user confirms
10. **MANDATORY**: Log user's response in audit.md with complete raw input

## Workflow Planning (ALWAYS EXECUTE)

1. **MANDATORY**: Log any user input during this phase in audit.md
2. Load all steps from `inception/workflow-planning.md`
3. **MANDATORY**: Load content validation rules from `common/content-validation.md`
4. Load all prior context:
   - Reverse engineering artifacts (if brownfield)
   - Intent analysis
   - Requirements (if executed)
   - User stories (if executed)
5. Execute workflow planning:
   - Determine which phases to execute
   - Determine depth level for each phase
   - Create multi-package change sequence (if brownfield)
   - Generate workflow visualization (VALIDATE Mermaid syntax before writing)
6. **MANDATORY**: Validate all content before file creation per content-validation.md rules
7. **Wait for Explicit Approval**: Present recommendations using language from workflow-planning.md Step 9, emphasizing user control to override recommendations - DO NOT PROCEED until user confirms
8. **MANDATORY**: Log user's response in audit.md with complete raw input

## Application Design (CONDITIONAL)

**Execute IF**:
- New components or services needed
- Component methods and business rules need definition
- Service layer design required
- Component dependencies need clarification

**Skip IF**:
- Changes within existing component boundaries
- No new components or methods
- Pure implementation changes

**Execution**:
1. **MANDATORY**: Log any user input during this phase in audit.md
2. Load all steps from `inception/application-design.md`
3. Load reverse engineering artifacts (if brownfield)
4. Execute at appropriate depth (minimal/standard/comprehensive)
5. **Wait for Explicit Approval**: Present detailed completion message (see application-design.md for message format) - DO NOT PROCEED until user confirms
6. **MANDATORY**: Log user's response in audit.md with complete raw input

## Units Generation (CONDITIONAL)

**Execute IF**:
- System needs decomposition into multiple units of work
- Multiple services or modules required
- Complex system requiring structured breakdown

**Skip IF**:
- Single simple unit
- No decomposition needed
- Straightforward single-component implementation

**Execution**:
1. **MANDATORY**: Log any user input during this phase in audit.md
2. Load all steps from `inception/units-generation.md`
3. Load reverse engineering artifacts (if brownfield)
4. Execute at appropriate depth (minimal/standard/comprehensive)
5. **Wait for Explicit Approval**: Present detailed completion message (see units-generation.md for message format) - DO NOT PROCEED until user confirms
6. **MANDATORY**: Log user's response in audit.md with complete raw input

---

# 🟢 CONSTRUCTION PHASE

**Purpose**: Detailed design, NFR implementation, and code generation

**Focus**: Determine HOW to build it

**Stages in CONSTRUCTION PHASE**:
- Per-Unit Loop (executes for each unit):
  - Functional Design (CONDITIONAL, per-unit)
  - NFR Requirements (CONDITIONAL, per-unit)
  - NFR Design (CONDITIONAL, per-unit)
  - Infrastructure Design (CONDITIONAL, per-unit)
  - Code Generation (ALWAYS, per-unit)
- Build and Test (ALWAYS - after all units complete)

**Note**: Each unit is completed fully (design + code) before moving to the next unit.

---

## Per-Unit Loop (Executes for Each Unit)

**For each unit of work, execute the following stages in sequence:**

### Functional Design (CONDITIONAL, per-unit)

**Execute IF**:
- New data models or schemas
- Complex business logic
- Business rules need detailed design

**Skip IF**:
- Simple logic changes
- No new business logic

**Execution**:
1. **MANDATORY**: Log any user input during this stage in audit.md
2. Load all steps from `construction/functional-design.md`
3. Execute functional design for this unit
4. **MANDATORY**: Present standardized 2-option completion message as defined in functional-design.md - DO NOT use emergent 3-option behavior
5. **Wait for Explicit Approval**: User must choose between "Request Changes" or "Continue to Next Stage" - DO NOT PROCEED until user confirms
6. **MANDATORY**: Log user's response in audit.md with complete raw input

### NFR Requirements (CONDITIONAL, per-unit)

**Execute IF**:
- Performance requirements exist
- Security considerations needed
- Scalability concerns present
- Tech stack selection required

**Skip IF**:
- No NFR requirements
- Tech stack already determined

**Execution**:
1. **MANDATORY**: Log any user input during this stage in audit.md
2. Load all steps from `construction/nfr-requirements.md`
3. Execute NFR assessment for this unit
4. **MANDATORY**: Present standardized 2-option completion message as defined in nfr-requirements.md - DO NOT use emergent behavior
5. **Wait for Explicit Approval**: User must choose between "Request Changes" or "Continue to Next Stage" - DO NOT PROCEED until user confirms
6. **MANDATORY**: Log user's response in audit.md with complete raw input

### NFR Design (CONDITIONAL, per-unit)

**Execute IF**:
- NFR Requirements was executed
- NFR patterns need to be incorporated

**Skip IF**:
- No NFR requirements
- NFR Requirements was skipped

**Execution**:
1. **MANDATORY**: Log any user input during this stage in audit.md
2. Load all steps from `construction/nfr-design.md`
3. Execute NFR design for this unit
4. **MANDATORY**: Present standardized 2-option completion message as defined in nfr-design.md - DO NOT use emergent behavior
5. **Wait for Explicit Approval**: User must choose between "Request Changes" or "Continue to Next Stage" - DO NOT PROCEED until user confirms
6. **MANDATORY**: Log user's response in audit.md with complete raw input

### Infrastructure Design (CONDITIONAL, per-unit)

**Execute IF**:
- Infrastructure services need mapping
- Deployment architecture required
- Cloud resources need specification

**Skip IF**:
- No infrastructure changes
- Infrastructure already defined

**Execution**:
1. **MANDATORY**: Log any user input during this stage in audit.md
2. Load all steps from `construction/infrastructure-design.md`
3. Execute infrastructure design for this unit
4. **MANDATORY**: Present standardized 2-option completion message as defined in infrastructure-design.md - DO NOT use emergent behavior
5. **Wait for Explicit Approval**: User must choose between "Request Changes" or "Continue to Next Stage" - DO NOT PROCEED until user confirms
6. **MANDATORY**: Log user's response in audit.md with complete raw input

### Code Generation (ALWAYS EXECUTE, per-unit)

**Always executes for each unit**

**Code Generation has two parts within one stage**:
1. **Part 1 - Planning**: Create detailed code generation plan with explicit steps
2. **Part 2 - Generation**: Execute approved plan to generate code, tests, and artifacts

**Execution**:
1. **MANDATORY**: Log any user input during this stage in audit.md
2. Load all steps from `construction/code-generation.md`
3. **PART 1 - Planning**: Create code generation plan with checkboxes, get user approval
4. **PART 2 - Generation**: Execute approved plan to generate code for this unit
5. **MANDATORY**: Present standardized 2-option completion message as defined in code-generation.md - DO NOT use emergent behavior
6. **Wait for Explicit Approval**: User must choose between "Request Changes" or "Continue to Next Stage" - DO NOT PROCEED until user confirms
7. **MANDATORY**: Log user's response in audit.md with complete raw input

---

## Build and Test (ALWAYS EXECUTE)

1. **MANDATORY**: Log any user input during this phase in audit.md
2. Load all steps from `construction/build-and-test.md`
3. Generate comprehensive build and test instructions:
   - Build instructions for all units
   - Unit test execution instructions
   - Integration test instructions (test interactions between units)
   - Performance test instructions (if applicable)
   - Additional test instructions as needed (contract tests, security tests, e2e tests)
4. Create instruction files in build-and-test/ subdirectory: build-instructions.md, unit-test-instructions.md, integration-test-instructions.md, performance-test-instructions.md, build-and-test-summary.md
5. **Wait for Explicit Approval**: Ask: "**Build and test instructions complete. Ready to proceed to Operations stage?**" - DO NOT PROCEED until user confirms
6. **MANDATORY**: Log user's response in audit.md with complete raw input

---

# 🟡 OPERATIONS PHASE

**Purpose**: Placeholder for future deployment and monitoring workflows

**Focus**: How to DEPLOY and RUN it (future expansion)

**Stages in OPERATIONS PHASE**:
- Operations (PLACEHOLDER)

---

## Operations (PLACEHOLDER)

**Status**: This stage is currently a placeholder for future expansion.

The Operations stage will eventually include:
- Deployment planning and execution
- Monitoring and observability setup
- Incident response procedures
- Maintenance and support workflows
- Production readiness checklists

**Current State**: All build and test activities are handled in the CONSTRUCTION phase.

## Key Principles

- **Adaptive Execution**: Only execute stages that add value
- **Transparent Planning**: Always show execution plan before starting
- **User Control**: User can request stage inclusion/exclusion
- **Progress Tracking**: Update aidlc-state.md with executed and skipped stages
- **Complete Audit Trail**: Log ALL user inputs and AI responses in audit.md with timestamps
  - **CRITICAL**: Capture user's COMPLETE RAW INPUT exactly as provided
  - **CRITICAL**: Never summarize or paraphrase user input in audit log
  - **CRITICAL**: Log every interaction, not just approvals
- **Quality Focus**: Complex changes get full treatment, simple changes stay efficient
- **Content Validation**: Always validate content before file creation per content-validation.md rules
- **NO EMERGENT BEHAVIOR**: Construction phases MUST use standardized 2-option completion messages as defined in their respective rule files. DO NOT create 3-option menus or other emergent navigation patterns.

## MANDATORY: Plan-Level Checkbox Enforcement

### MANDATORY RULES FOR PLAN EXECUTION
1. **NEVER complete any work without updating plan checkboxes**
2. **IMMEDIATELY after completing ANY step described in a plan file, mark that step [x]**
3. **This must happen in the SAME interaction where the work is completed**
4. **NO EXCEPTIONS**: Every plan step completion MUST be tracked with checkbox updates

### Two-Level Checkbox Tracking System
- **Plan-Level**: Track detailed execution progress within each stage
- **Stage-Level**: Track overall workflow progress in aidlc-state.md
- **Update immediately**: All progress updates in SAME interaction where work is completed

## Prompts Logging Requirements
- **MANDATORY**: Log EVERY user input (prompts, questions, responses) with timestamp in audit.md
- **MANDATORY**: Capture user's COMPLETE RAW INPUT exactly as provided (never summarize)
- **MANDATORY**: Log every approval prompt with timestamp before asking the user
- **MANDATORY**: Record every user response with timestamp after receiving it
- **CRITICAL**: ALWAYS append changes to EDIT audit.md file, NEVER use tools and commands that completely overwrite its contents
- **CRITICAL**: NEVER use file writing tools and commands that overwrite the entire contents of audit.md, as this causes duplication
- Use ISO 8601 format for timestamps (YYYY-MM-DDTHH:MM:SSZ)
- Include stage context for each entry

### Audit Log Format:
```markdown
## [Stage Name or Interaction Type]
**Timestamp**: [ISO timestamp]
**User Input**: "[Complete raw user input - never summarized]"
**AI Response**: "[AI's response or action taken]"
**Context**: [Stage, action, or decision made]

---
```

### Correct Tool Usage for audit.md

✅ CORRECT:

1. Read the audit.md file
2. Append/Edit the file to make changes

❌ WRONG:

1. Read the audit.md file
2. Completely overwrite the audit.md with the contents of what you read, plus the new changes you want to add to it

## Directory Structure

```text
<WORKSPACE-ROOT>/                   # ⚠️ APPLICATION CODE HERE
├── [project-specific structure]    # Varies by project (see code-generation.md)
│
├── aidlc-docs/                     # 📄 DOCUMENTATION ONLY
│   ├── inception/                  # 🔵 INCEPTION PHASE
│   │   ├── plans/
│   │   ├── reverse-engineering/    # Brownfield only
│   │   ├── requirements/
│   │   ├── user-stories/
│   │   └── application-design/
│   ├── construction/               # 🟢 CONSTRUCTION PHASE
│   │   ├── plans/
│   │   ├── {unit-name}/
│   │   │   ├── functional-design/
│   │   │   ├── nfr-requirements/
│   │   │   ├── nfr-design/
│   │   │   ├── infrastructure-design/
│   │   │   └── code/               # Markdown summaries only
│   │   └── build-and-test/
│   ├── operations/                 # 🟡 OPERATIONS PHASE (placeholder)
│   ├── aidlc-state.md
│   └── audit.md
```

**CRITICAL RULE**:
- Application code: Workspace root (NEVER in aidlc-docs/)
- Documentation: aidlc-docs/ only
- Project structure: See code-generation.md for patterns by project type

---

## Project Context: opcua-mcp

Everything below reflects this repo's *current, verified* state after a completed hardening pass over critical correctness (data races, stdio log corruption, wire-type decoding, input validation) and performance/robustness (partial-batch reads, browse pagination, discovery cache correctness, graceful shutdown) issues - not aspirations. The ad-hoc audit docs that drove that pass (`docs/plan/findings.md`/`plan.md`/`decisions.md`) have been retired now that their items have landed; `docs/COMMIT_CONVENTION.md` (this repo's commit message convention) is the one doc from that effort still in `docs/`. Further work is planned and tracked through the AIDLC workflow described at the top of this file - see "Where to look for current work" below.

### What this is

`opcua-mcp` is a Go MCP (Model Context Protocol) server that bridges LLM clients to OPC-UA industrial automation servers. It exposes OPC-UA read/write/browse/discovery operations as MCP tools over stdio or HTTP transport, and maintains a searchable (Bleve full-text) cache of the server's address space via background discovery.

### Architecture

- `cmd/opcua-mcp.go` - entrypoint. Loads config, initializes the logger, constructs `internal/opcua.Client`, connects eagerly unless `SERVER_TRANSPORT=stdio` (stdio connects lazily via the `opcua_connect` tool), constructs `internal/mcp.Server`, starts it.
- `internal/mcp/server.go` - `Server` type. Registers all 15 MCP tools in `SetupTools()` (none removed or gated behind a flag yet), holds the `*opcua.Client` and `*opcua.DiscoveryService`, runs the stdio or HTTP transport loop, handles graceful shutdown (`setupGracefulShutdown`) - which now also drains the running `*server.StreamableHTTPServer` (stored on `Server`, guarded by `httpServerMu`) before exiting in http mode.
- `internal/opcua/client.go` - `Client` type. Thin wrapper over `gopcua/opcua`. The `client` field is typed as the package-internal `opcuaClient` interface (`Connect`/`Close`/`Read`/`Write`/`Browse`/`BrowseNext`), not the concrete `*opcua.Client`, specifically so tests can inject a mock - reuse `internal/opcua/mock_client_test.go`'s `mockOpcuaClient` for new tests rather than adding a second seam. `client`/`connected` are guarded by `mu sync.RWMutex`; every method reads them via the `snapshot()` helper rather than touching the fields directly (`grep -n "c\.client\b\|c\.connected\b" internal/opcua/client.go` should only show hits inside `snapshot()`/`Connect()`/`Disconnect()`/`SetConnectedForTesting()`). `Read` returns a result per requested node even when some have a bad status - only a transport-level failure produces a top-level error. `Browse` follows `BrowseNext` continuation points (bounded by both `ctx` and a hard iteration cap). `Write` fetches `GetNodeTypeInfo` once and passes it into `ValidateValueForNode` (no redundant second fetch), and returns an error - not just a logged warning - when validation fails.
- `internal/opcua/discovery.go` - `DiscoveryService` type. Walks the OPC-UA address space from `SEARCH_DISCOVERY_ROOT_NODE` on a ticker (`SEARCH_DISCOVERY_INTERVAL`), maintains an in-memory `nodeCache` (protected by `cacheMutex sync.RWMutex`) and a persistent on-disk Bleve full-text index. Discovery is generation-tagged mark-and-sweep: `discoveryMu` serializes the ticker worker against an explicit `opcua_force_discovery` call (only one walk runs at a time); `walkGen`/`NodeInfo.SeenInGen` tag every node touched in the current walk; a sweep pass after the walk deletes anything not touched from both `nodeCache` and the Bleve index in the same batch. The cache is upserted in place rather than wiped-then-rebuilt, and a walk-scoped `visited` set stops a subtree shared by multiple parents (address spaces are graphs, not trees) from being re-browsed once per incoming reference. **Known gap, not yet fixed**: `GetNodeByBrowseName` and the tiered Bleve search methods (`searchExact`/`searchWildcard`/`searchFuzzy`, `SearchNodes`, `SearchByDepth`, `SearchByNodeClass`) returned zero hits for some queries that should match, in testing - `browse_name` is mapped with both a text and a keyword analyzer on the same field path in `NewDiscoveryService`, which is the confirmed root cause for that field; a couple of other query paths (numeric depth range, node_class exact term) also came back empty without a confirmed root cause yet. Don't rely on these search tiers for new behavior until this is investigated.
- `internal/config/` - config via `spf13/viper`, five structs (`ServerConfig`/`SERVER_*`, `OPCUAConfig`/`OPCUA_*`, `MCPConfig`/`MCP_*`, `SearchConfig`/`SEARCH_*`, `StoreConfig`/`STORE_*`), loaded once in `cmd/opcua-mcp.go`. Each field carries a `mapstructure:"..."` tag (snake_case, no prefix) rather than the old `env`/`envDefault` tags; defaults are registered explicitly in `setDefaults()` rather than via struct tags, and must stay in sync with it. Precedence is defaults < optional config file (TOML/YAML/JSON/etc, `./config.*` or `CONFIG_FILE=<path>`) < environment variables - env vars always win, preserving every existing `SERVER_*`/`OPCUA_*`/`MCP_*`/`SEARCH_*`/`STORE_*` name as-is. `Load()` builds a fresh `viper.New()` instance per call rather than using viper's global instance, so tests that set/unset env vars don't leak state into each other.
- `internal/logger/` - `slog`-based structured logging. `SERVER_TRANSPORT=stdio` forces the output writer to stderr regardless of `SERVER_LOG_OUTPUT` (`resolveLogOutput`), with a one-time warning if the user explicitly set `SERVER_LOG_OUTPUT=stdout`, since stdout carries the MCP JSON-RPC stream in stdio mode.

### Build, test, run

```sh
go build ./...
go vet ./...
go test ./...
go test -race ./...          # exercises Client's connection state and discovery's cache from multiple goroutines
gofmt -l .                   # should output nothing
golint ./...                 # should output nothing; CI runs this too (.github/workflows/go-ci.yml)
```

Run locally, stdio mode (default transport, no OPC-UA connection until `opcua_connect` is called):
```sh
OPCUA_ENDPOINT=opc.tcp://localhost:4840 go run ./cmd/opcua-mcp
```
Run locally, HTTP mode (connects to the OPC-UA server eagerly at startup):
```sh
SERVER_TRANSPORT=http SERVER_HTTP_PORT=8080 OPCUA_ENDPOINT=opc.tcp://localhost:4840 go run ./cmd/opcua-mcp
```

`golangci-lint`/`staticcheck` are not reliably available in every dev environment - don't assume their absence means the code is clean; `go vet`/`gofmt`/`golint`/tests are the reliable local baseline.

### Hard rules

- **All code shall be linted.** Run `golint ./...` locally before considering work done and fix findings rather than leaving them for CI to catch - CI's `go-ci.yml` workflow runs `golint ./...` and fails the build on any output. If a finding conflicts with an intentional design pattern (e.g. an unexported interface used as a test seam), resolve the conflict explicitly (rename/export, or a narrowly-scoped CI exclusion) rather than silently ignoring it - classic `golint` has no `//nolint` suppression.
- **Never write to stdout in stdio mode.** Enforced today: `internal/logger.resolveLogOutput` forces stderr whenever `SERVER_TRANSPORT=stdio`, regardless of `SERVER_LOG_OUTPUT`. Don't reintroduce a stdout path in logger setup or stdio transport code.
- **No new direct dependencies without explicit sign-off.** Promoting `go.etcd.io/bbolt` (already an indirect dependency via Bleve) to direct is pre-approved for upcoming persistent-cache/subscription work.
- **Preserve env var and tool-name backward compatibility.** All 15 currently-registered tools are still present and ungated; any removal or gating (e.g. behind a feature flag) needs explicit sign-off and a README changelog callout, same bar as the `opcua_write`/stdio-logging behavior changes already called out there.
- **Guard `Client.client`/`Client.connected` with `Client.mu`.** Every method must snapshot state via the `snapshot()` helper rather than reading the fields directly; don't add a new concurrent access path to `Client` without going through it.
- **Don't call `store`/Bleve methods while holding `DiscoveryService.cacheMutex`** (and, once a `SubscriptionManager` exists, its own lock) - snapshot needed data under the lock, release, then do I/O. `discoveryMu` is held for a whole walk but never across a Bleve I/O call outside `updateSearchIndex`'s existing batch pattern - keep it that way.
- **Never use em-dashes (`—`)**, anywhere - code comments, docs, commit messages, PR descriptions, chat responses. Use a hyphen (`-`), a comma, or split into two sentences instead. Em-dashes read as AI-generated slop.

### Conventions

- Error wrapping: `fmt.Errorf("...: %w", err)`.
- All logging goes through `internal/logger` (a thin `slog` wrapper) - never `fmt.Print*`/`log.Print*` directly.
- Tests are table-driven (see `internal/config/config_test.go` for the style). Mock the gopcua client via the `opcuaClient` interface seam (`internal/opcua/mock_client_test.go`) rather than a live/simulated server; `internal/opcua/discovery_test.go`'s `treeBrowser` builds on the same seam for discovery-specific tests.
- New MCP tools are registered in `internal/mcp/server.go`'s `SetupTools()`, each with a `mcp.NewTool(...)` definition and a `handleX` method; keep tool descriptions distinct - `opcua_read`/`opcua_get_value` and `opcua_browse`/`opcua_browse_nodes` already overlap functionally (one is a strict subset of the other), a known gap not yet cleaned up.

### Where to look for current work

Planning happens through the AIDLC workflow at the top of this file, not ad-hoc plan docs. Check `aidlc-docs/` for the current requirements/design/plan state once a workflow session has produced one; `git log` is the source of truth for what's already landed (each commit follows `docs/COMMIT_CONVENTION.md` and names the plan item or finding it addresses where applicable).

If you're picking up a plan item, re-verify its cited file:line references before trusting them - the code may have moved since the audit.
