# Grip — Product Requirements Document

**Status:** Draft v0.1 · **Type:** PRD · **Owner:** (you) · **Companion doc:** *Architecture Control Plane — Specification* (deep spec for the architecture plane)

**One line:** Grip is a deterministic control plane that lets an engineer stay the architect of a system while AI agents do the implementation — declaring intent at the boundaries, deriving the real state from the code, and gating every change that would move the system's shape without consent.

---

## 1. Summary

Grip governs four axes along which AI-speed development silently erodes control — **architecture, behavior, contracts, and test rigor** — using one shared engine and one gate. For each axis, the human declares a small intent, Grip derives the actual state directly from the code on every change, and a deterministic gate blocks anything that drifts from the declared intent unless the human deliberately ratifies the change. The result: the agent can write as fast as it likes, but it cannot alter the structure, behavior, external contracts, or test-meaning of the system without a visible, reviewable, consented change. The engineer reviews the *shape* of each change in seconds, not the lines.

## 2. Problem and motivation

### 2.1 The problem
AI coding agents produce correct-looking code faster than any human can review it. The bottleneck has moved from writing code to understanding, containing, and trusting it. Line-by-line review does not scale to agent output, so it gets skipped — and with it goes the engineer's grip on the system. Drift then accumulates across several independent axes at once, one plausible diff at a time, with nobody deciding it:

- **Architecture drift:** boundaries blur, dependencies sprawl, public interfaces widen.
- **Behavioral drift:** a rewrite quietly changes what the system does, while still compiling and passing shallow tests.
- **Contract drift:** an API field, event shape, or database schema changes in a way that breaks consumers.
- **Test-rigor drift:** coverage stays green while becoming meaningless — vacuous assertions, over-mocking, skipped or deleted tests, lowered thresholds.

### 2.2 Why existing tools fall short
Spec-driven approaches make a written spec the source of truth, but the spec is a second artifact that rots the moment it diverges from code. Diagram-and-model tools make a picture authoritative, and the picture drifts from the code the same way. Point tools exist for several of these axes individually (dependency linters, contract testing, schema-compat checkers, mutation testing), but they are separate, separately configured, and leave gaps between them that an agent slips through — and none of them present a single consented gate or a single shape-level review surface.

### 2.3 Why now
AI-generated code is being merged at volume, its defect and vulnerability rates are well documented, and trust in agent output is the acknowledged bottleneck. The teams shipping fastest are precisely the ones most exposed to silent multi-axis drift. The opportunity is to give the human back control at a higher level of abstraction than the line.

## 3. Vision and strategy

### 3.1 The core pattern
Everything Grip does is one repeated loop:

> **Declare** a small intent at a boundary → **derive** the actual state from the code → run a deterministic **gate** on the delta → let the human review the **shape**, not the lines.

### 3.2 The product thesis
Grip is **one engine and four policy planes**, not four tools. The engine (derive, reconcile, gate, diff/report) is generic; each plane is a plugin that supplies a manifest schema, a deriver, and a set of enforcement rules. This yields the three things a bundle of point tools cannot: one gate (a change clears all planes or it does not land — no gaps), one review surface (a single diff showing structure, behavior, contract, and test-rigor deltas together), and one manifest home per module (all declarations co-located and versioned with the code they govern).

### 3.3 What "grip" means concretely
The engineer can (a) state the intended shape of the system in artifacts small enough to actually maintain, (b) have every change checked against that intent automatically and deterministically, (c) see per change exactly how the shape moved, and (d) evolve the intent deliberately and auditably when the design genuinely needs to change. Grip is *not* reviewing every line and *not* slowing the agent down.

## 4. Goals, non-goals, and success metrics

### 4.1 Product goals
1. Keep intended and actual state provably equal across all four planes, with drift surfaced as an enforceable failure.
2. Reduce the human's per-change review surface from "all the code" to "the shape delta."
3. Make module internals safely ignorable by anchoring trust at verified boundaries.
4. Impose near-zero authoring overhead — declarations stay small enough to read in seconds.
5. Be adoptable incrementally: value at plane one, and plane-by-plane onto an existing codebase.
6. Integrate with existing tooling (version control, test runners, analyzers, agents) rather than replacing it.

### 4.2 Success metrics
| Metric | Target |
|--------|--------|
| Un-ratified structural / behavioral / contract changes reaching main | Zero |
| Share of changes the architect reviews via shape-diff rather than line-diff | Majority, trending up |
| Median manifest size per module | Small enough to read in under a minute |
| Gate false-block rate (hard tier) | Low enough not to be routinely bypassed |
| Time to onboard an existing repo to plane one | Short (baseline in one sitting) |
| Modules that hide internals but carry a verified boundary contract | Approaching all governed modules |
| Gate bypass rate in normal use | Near zero |

### 4.3 Non-goals
- Not an IDE, editor, or coding agent. Grip governs; it does not author code.
- Not a spec system that describes behavior in prose detail. Manifests declare structure and contract, not implementation.
- Not a visual authoring studio. Any diagram is read-only and derived, never an editable master. (Hardest scope line; see §15.)
- Not a general code-quality/style suite. Grip governs boundaries, behavior, contracts, and test-meaning — not formatting or exhaustive linting that existing tools already do.
- Not a replacement for human judgment on semantics. Grip verifies a module stays in its declared shape and passes its contract; it cannot verify the shape and contract were the *right* ones.

## 5. Target users and jobs to be done

### 5.1 Personas
- **Primary — The Architect.** A senior or solo engineer shipping with heavy AI assistance who wants to stay in control of the system's structure and behavior without reading every generated line. Values determinism, small artifacts, and being able to say "no" to a change at a glance.
- **Secondary — The Small Team.** A handful of engineers adopting shared guardrails so that everyone's agents are held to the same declared architecture and contracts.

### 5.2 Not the target
- Teams that want a fully autonomous, no-human-gate flow.
- Beginners looking for an IDE or a zero-setup experience.
- Codebases where no one is willing to declare and own boundaries.

### 5.3 Jobs to be done
- "Let my agent move fast but never let it silently change the shape, behavior, or external contracts of my system."
- "Let me trust a module as a black box without reading its internals."
- "Show me what a change did to my system in seconds, so I can approve or reject it."
- "When I genuinely want to change the design, let me do it cheaply and leave an auditable record."

## 6. Product principles (tenets)

These are load-bearing; any feature that violates one is rejected.

1. **Truth is derived from code, never authored beside it.** No plane maintains a copy of state that could diverge from the source.
2. **The human authors only small things, and only at the boundary.** If an artifact is as much work to review as the code, it has failed.
3. **Enforcement is deterministic; the LLM only advises.** Anything that blocks a merge is a mechanical, reproducible check. AI judgments inform and warn, never gate.
4. **The boundary is the unit of trust.** Verification and encapsulation attach to public interfaces; internals are trusted because boundaries are verified.
5. **Rigidity applies to accident, not intent.** Accidental drift is blocked; intentional change is a cheap, first-class, recorded manifest edit.
6. **The gate fails closed.** Ambiguity, missing declarations, or analysis failure default to block. Silence is never approval.
7. **Every plane and stage stands alone.** Each delivers real control even if the next is never built.
8. **One gate, one review surface, one manifest home.** The planes unify at the point of decision and the point of review.

## 7. Scope — the shared engine and four planes

### 7.1 Shared engine
The engine is plane-agnostic and provides:
- **Manifest** — the small, co-located, human-authored declaration for a module; each plane contributes its own section.
- **Derive** — extracts the real per-axis state from source (and version-control history where relevant), deterministically and reproducibly.
- **Reconcile** — compares declared intent against derived state, producing a located, explained set of violations.
- **Gate** — orchestrates derive → reconcile → contract/verification checks at a control point (pre-commit, pre-merge/CI), applies the tiered policy, returns pass/block with reasons, and fails closed.
- **Diff and report** — presents each change as a unified shape delta across all planes, and produces actionable blocked-change reports.
- **Plane plugin contract** — the interface a plane implements: a manifest schema, a deriver, and a tiered rule set. New planes plug in without touching the engine.

### 7.2 Plane 1 — Architecture
**Governs:** module boundaries, allowed dependencies, and public interfaces. **Human declares:** each module's intent (responsibility and anti-responsibilities), its facade (deliberately exposed surface), and its allowed outbound dependencies. **Grip derives:** the real dependency graph and the real exported surface. **Blocks (hard):** illegal dependency, facade widening (undeclared export), dependency cycle, direction/layer violation, external code reaching a module's internals. **Warns:** duplication, co-change coupling, excessive delegation, message chains, single-implementor abstractions. *(Detailed mechanics in the companion ACP spec.)*

### 7.3 Plane 2 — Behavior
**Governs:** what the system actually does at its boundaries, closing the architecture plane's semantic blind spot. **Human declares:** nothing up front; they ratify deltas. **Grip derives:** characterization/approval snapshots of observed boundary behavior captured from real runs. **Blocks (hard):** a change that alters pinned boundary behavior without an accompanying ratification. **Warns:** newly observed behaviors not yet pinned; behavior in modules lacking any snapshot. **Value:** when the agent rewrites internals, any behavioral change surfaces as a diff to approve or reject rather than a silent shift.

### 7.4 Plane 3 — Contracts
**Governs:** the boundaries at the wire — service APIs, event/message schemas, and database schema. **Human declares:** the intended external contract (or adopts the current one as baseline). **Grip derives:** the actual contract shape from code and schema definitions, and compares against declared and previous versions. **Blocks (hard):** backward-incompatible changes — removed/renamed fields in use, incompatible migrations, event-shape breaks — against declared compatibility rules. **Warns:** additive changes and deprecations pending consumer sign-off. **Value:** an agent cannot ship a consumer-breaking change without a red build and a "this breaks X" report.

### 7.5 Plane 4 — Test rigor
**Governs:** whether the tests the other planes lean on actually mean anything — the trust anchor for the whole system. **Human declares:** what "really covered" means for a module (which behaviors must be meaningfully tested). **Grip derives:** test effectiveness via mutation testing (do tests fail when the code is broken?), plus detection of skipped/deleted tests, over-mocking of the unit under test, and coverage-threshold tampering. **Blocks (hard):** a boundary contract that survives mutation (vacuous), a silently deleted or skipped required test, a lowered threshold on a governed module. **Warns:** declining mutation scores, rising mock ratios. **Value:** "covered with tests" becomes a claim you can bank, which is what makes the black-box trust model honest.

## 8. Functional requirements

### 8.1 Shared engine
| ID | Requirement | Priority |
|----|-------------|----------|
| GR-ENG-1 | Extract the actual per-axis state from source on every governed change, deterministically. | Must |
| GR-ENG-2 | Support per-module manifests, co-located with code and version-controlled, with a section per plane. | Must |
| GR-ENG-3 | Reconcile declared intent against derived state and produce located, one-sentence-explainable violations. | Must |
| GR-ENG-4 | Provide a single gate that runs all enabled planes and returns one pass/block decision, failing closed. | Must |
| GR-ENG-5 | Produce a unified per-change shape diff spanning all enabled planes. | Must |
| GR-ENG-6 | Expose a plane plugin contract (schema + deriver + rules) so planes are added without engine changes. | Must |
| GR-ENG-7 | Let the human change intended state by editing a manifest, recorded as an auditable design decision. | Must |
| GR-ENG-8 | Report ungoverned modules (no manifest) distinctly from governed ones. | Should |
| GR-ENG-9 | Support importing an existing codebase by generating draft manifests/baselines from current state for human ratification. | Should |
| GR-ENG-10 | Report reduced confidence where static analysis cannot see reliably, rather than a false clean result. | Should |

### 8.2 Per-plane
| ID | Requirement | Priority |
|----|-------------|----------|
| GR-ARC-1 | Block illegal dependencies, facade widening, cycles, direction/layer violations, and internal-reach. | Must |
| GR-ARC-2 | Surface architecture advisories (duplication, co-change, delegation, message chains, speculative generality) without blocking. | Should |
| GR-BEH-1 | Capture boundary-behavior snapshots from real runs and block un-ratified changes to pinned behavior. | Must |
| GR-BEH-2 | Flag boundary behaviors that are observed but not yet pinned. | Should |
| GR-CON-1 | Detect and block backward-incompatible API, event-schema, and database-schema changes against declared compatibility rules. | Must |
| GR-CON-2 | Report additive/deprecating contract changes as advisory pending sign-off. | Should |
| GR-TST-1 | Verify boundary contract tests via mutation testing and block vacuous contracts. | Must |
| GR-TST-2 | Detect and block silently skipped/deleted required tests and threshold tampering on governed modules. | Must |
| GR-TST-3 | Report modules that hide internals but lack a verified boundary contract as unverified. | Must |

### 8.3 Cross-cutting
| ID | Requirement | Priority |
|----|-------------|----------|
| GR-X-1 | CLI-first operation: run the full gate, any single plane, and the diff/report from the command line. | Must |
| GR-X-2 | Integrate at multiple control points: local pre-commit (fast) and CI pre-merge (authoritative). | Must |
| GR-X-3 | Per-repo configuration selecting which planes are enabled and which advisory rules are promoted to hard gates. | Must |
| GR-X-4 | A ratify/baseline path to accept current reality as the declared starting point per plane. | Must |
| GR-X-5 | Blocked-change reports naming the exact rule, location, and remedy in plain language. | Must |
| GR-X-6 | An LLM-assisted advisory pass for judgment-based smells (e.g. unclear names), clearly marked non-blocking. | Could |
| GR-X-7 | A read-only visual of the architecture and per-change deltas. | Could |

## 9. Enforcement policy model

Enforcement is **tiered** across every plane so that only deterministic, low-false-positive checks ever block a merge. This directly implements principle §6.3.

- **Tier A — Hard gates (deterministic; block):** the "Blocks (hard)" items listed per plane in §7, plus fail-closed conditions (missing manifest on a governed module, analysis error).
- **Tier B — Advisory signals (deterministic; reported, do not block by default):** the "Warns" items per plane. Any may be promoted to Tier A per repo, but the default is warn-and-record so the architect decides.
- **Tier C — Judgment-assisted (LLM or human; advisory only, never blocking):** unclear names, suspected data clumps, primitive obsession, ambiguous feature envy — semantic concerns with false-positive risk that the tool cannot verify.

## 10. Trust model

The central claim — that internals can be safely ignored — must be earned, and tests alone do not earn it: an agent that misunderstands a requirement tends to write both wrong internals and a passing test encoding the same misunderstanding (correlated failure). Grip earns the claim three ways. **The boundary is the contract:** trust attaches to behavior observed through the facade, keeping the human's verification surface small. **Independence breaks correlation:** contracts can be authored or reviewed in a pass separate from implementation, and property-based tests explore inputs the implementer did not hand-pick; the test-rigor plane then verifies (via mutation) that the contract actually bites. **Unearned trust is made visible:** a module that hides internals with no verified contract is reported as unverified — encapsulation never masquerades as verification. What the human still owns irreducibly: deciding whether a module's declared intent and contract are the *right* ones.

## 11. User experience and key workflows

- **Onboarding an existing repo.** The engineer runs Grip against the codebase; it derives current state per enabled plane and generates draft manifests/baselines. The engineer ratifies (accept current reality as the declared starting point), and governance begins from there. This avoids an unfixable initial violation backlog (§15).
- **Day-to-day change.** The agent makes a change → the local gate runs fast on commit → CI runs the authoritative gate on the merge → the change either lands or is blocked with a plain report → the engineer reviews the unified shape diff, not the lines.
- **Deliberately evolving intent.** To change the architecture, behavior baseline, or contract on purpose, the engineer edits the relevant manifest section; that edit *is* the design decision, appears in history, and the diff renders it as an intentional change ("the architect widened this facade / re-pinned this behavior on purpose").
- **Surfaces.** CLI-first (whole gate, single plane, or diff). Pre-commit and CI integrations. A read-only visual arrives last and never becomes an authoring surface.

## 12. Non-functional requirements

| ID | Requirement |
|----|-------------|
| NFR-1 | **Determinism** — identical results for a given commit, every run. |
| NFR-2 | **Zero drift by construction** — no component keeps a copy of state that can diverge from code. |
| NFR-3 | **Low authoring overhead** — manifests and contracts stay small enough to review in seconds to a minute. |
| NFR-4 | **Speed** — local gate fast enough to run on every commit; CI gate authoritative and may be more thorough. |
| NFR-5 | **Explainability** — every blocking decision names rule, location, and remedy in one plain sentence. |
| NFR-6 | **Fail-closed safety** — ambiguity or failure defaults to block. |
| NFR-7 | **Incremental adoptability** — value at plane one; add planes and modules independently. |
| NFR-8 | **Tooling reuse** — build on existing analyzers, contract/schema checkers, mutation frameworks, and runners. |
| NFR-9 | **Honest confidence** — report reduced confidence where analysis is unreliable. |
| NFR-10 | **Non-authoritative visualization** — any visual is read-only and derived. |
| NFR-11 | **Language/stack adaptability** — the engine is generic; per-stack derivers plug in per plane. |

## 13. Release plan

Built in stages; each stage stands alone (§6.7). Do not build a later stage until earlier ones are load-bearing.

- **M0 — Engine skeleton + Architecture plane (MVP).** Manifest format, derive/reconcile/gate/diff, and the architecture plane's hard gates (illegal deps, cycles, facade widening). Local + CI control points, CLI. Delivers most of the control on its own.
- **M1 — Test-rigor plane.** Mutation-based verification, skipped/deleted-test and threshold-tamper detection, unverified-module reporting. Built second because it makes every other plane's trust real.
- **M2 — Behavior plane.** Boundary snapshotting, ratify-on-delta, pinned-behavior gating. Closes the semantic blind spot.
- **M3 — Contract plane.** API/event/schema/migration compatibility gating, folded into the same gate and diff.
- **M4 — Advisories + read-only visualization.** Tier B/C advisory passes and the read-only architecture/diff visual. Last, and optional. Stop before it becomes a studio.

Across all milestones the custom build is small — chiefly the manifest format, the reconciler, the plane plugin contract, and the unified diff/report. Derivation, contract/schema checks, mutation testing, and the coding itself are assembled from existing tools.

## 14. Metrics and success criteria

Grip is working if: no structural, behavioral, or contract change reaches main without matching a manifest or being an explicit recorded ratification; the architect reviews most changes via the shape diff rather than the implementation; modules with hidden internals reliably carry verified boundary contracts, and unverified ones are visibly flagged; and evolving intent on purpose is a quick manifest edit. It is failing if manifests grow large, if the team routinely bypasses the gate, or if a visual layer has quietly become where people edit the design.

## 15. Risks and mitigations

| Risk | Mitigation |
|------|-----------|
| **Scope creep to a studio.** An editable authoritative visual reintroduces the drift Grip exists to kill. | Treat non-goals (§4.3) and principle §6.1 as non-negotiable; visualization stays read-only and last. |
| **Boundary rigidity vs emergent design.** Painful boundary changes ossify premature abstraction. | Principle §6.5: intentional change is a cheap, recorded manifest edit. Keep it genuinely cheap; watch in real use. |
| **Authoring overhead.** Large manifests/contracts return the human to full review. | Hard smallness requirement (NFR-3); prefer generate-then-ratify over author-from-scratch. |
| **Semantic drift invisible to the tool.** A module can do the wrong thing while staying in shape and passing contracts. | Behavior plane pins observable behavior; intent statements and Tier C advisories aid human notice; residual risk stays human-owned. |
| **Analysis blind spots.** Dynamic dispatch, reflection, cross-language/service calls hide real state. | Report reduced-confidence zones (NFR-9) rather than false clean results. |
| **Brownfield backlog.** Existing repos produce large first-run violation sets. | Ratify/baseline path (GR-X-4) accepts current reality as the starting declaration. |
| **Correlated failure in tests.** Wrong code plus wrong passing test. | Independence practices + mutation verification in the test-rigor plane (§10, GR-TST-1). |

## 16. Open questions

- Right granularity of a "module" per stack (directory, package, service), and its interaction with monorepo vs multi-repo layouts.
- How cross-module and cross-service contracts are owned and verified without pulling internals into scope.
- Managing false-positive cost when Tier B advisories are promoted to hard gates.
- Keeping the manifest authoritative for *intent* when intent is only partially machine-checkable.
- How behavior snapshots are captured cheaply and kept stable (flakiness, nondeterminism) without heavy instrumentation.

## 17. Out of scope / future

- Autonomous remediation (Grip blocks and explains; it does not auto-fix by default).
- Governance of the agents' own configs and permissions (a plausible future plane).
- Performance/resource budgets (bundle size, query counts, latency) as a future budget-style plane.
- Documentation sync as a separate system — deliberately excluded; executable rules replace rotting docs.

## 18. Glossary

**Grip** — the product; a deterministic control plane over four axes. **Plane** — a plugin governing one axis (architecture, behavior, contract, test rigor). **Manifest** — the small, co-located, human-authored per-module declaration; each plane contributes a section. **Facade / public surface** — a module's deliberately exposed names; everything else is internal. **Derived state** — actual per-axis state extracted from source each run. **Reconciliation** — comparison of declared vs derived, producing violations. **Boundary contract test** — a human-owned test exercising a module only through its facade. **Characterization/behavior snapshot** — captured record of observed boundary behavior, ratified on change. **Shape diff** — the per-change delta across all planes. **Gate** — the single deterministic pass/block decision point. **Tier A/B/C** — hard-blocking / advisory-deterministic / judgment-assisted enforcement. **Correlated failure** — the same misunderstanding producing both wrong code and a wrong passing test. **Fail closed** — defaulting to block on ambiguity or error. **Ratify / baseline** — accepting current derived reality as the declared starting point.
