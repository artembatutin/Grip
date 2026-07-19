# Architecture Control Plane — Specification

**Status:** Draft v0.1 · **Type:** System specification · **Scope:** Personal / single-team engineering tool

**One line:** A deterministic guardrail that sits between an AI coding agent and your main branch, and refuses any change that alters the *shape* of the system without your explicit consent.

---

## 1. Purpose

### 1.1 The problem

AI coding agents now produce correct-looking code faster than any human can review it. The bottleneck of software development has moved from *writing* code to *understanding, containing, and trusting* it. Line-by-line review does not scale to agent-speed output, so in practice it is skipped — and with it goes the engineer's grip on the system. Architecture erodes silently: boundaries blur, dependencies sprawl, public interfaces widen, and the codebase drifts away from any intended design. Nobody decided this; it accumulated one plausible diff at a time.

Existing responses fall into two camps, and both leak. Spec-driven approaches make a written specification the source of truth, but that spec is a second artifact that must be kept in sync with the code and rots the moment it isn't. Diagram-and-model tools make a picture authoritative, but the picture and the code drift apart for the same reason. In each case the intended structure and the real structure are two separate things, and keeping them equal is manual, unrewarded work that stops happening under deadline pressure.

### 1.2 The thesis

The engineer should operate as the **architect** — owning module boundaries, responsibilities, public interfaces, and allowed dependencies — while the agent implements the internals. For this division to hold, the architect must be able to trust a module as a black box: its behavior verified at the boundary, its structure kept honest automatically, its internals genuinely ignorable.

The Architecture Control Plane (ACP) makes that trust mechanical. It never asks the human to police generated code by reading it. Instead it derives the *actual* structure of the system directly from the code on every change, compares it against a small set of human-authored declarations, and blocks anything that violates them. The human authors intent; the machine enforces it; drift becomes a build failure rather than a slow decay.

### 1.3 What "control" means here

Control is the ability to (a) state the intended shape of the system in a form small enough to actually maintain, (b) have every change checked against that intent automatically and deterministically, (c) see, per change, exactly how the shape moved, and (d) evolve the intended shape deliberately and auditably when the design genuinely needs to change. Control is *not* reviewing every line, and it is *not* preventing the agent from writing code fast.

---

## 2. Goals and non-goals

### 2.1 Goals

- Keep the intended architecture and the real architecture provably equal, with drift surfaced as an enforceable failure.
- Reduce the human's per-change reviewing surface from "all the code" to "the boundary and the shape delta."
- Make module internals safely ignorable by anchoring trust at the public interface.
- Impose near-zero authoring overhead: the human-maintained artifacts must stay small enough to read in seconds.
- Be adoptable incrementally, delivering value at the first stage without requiring the whole system.
- Integrate with existing tooling (version control, existing test runners, existing static-analysis tools, existing agents) rather than replacing it.

### 2.2 Non-goals

- **Not** an IDE, editor, or coding agent. ACP governs; it does not author code.
- **Not** a specification system where prose describes behavior in detail. The manifest declares *structure and contract*, not implementation.
- **Not** a visual authoring studio. Any diagram is a read-only reflection of the code, never an editable master. (This is the single hardest scope line and the one most likely to be crossed accidentally; see §11.1.)
- **Not** a general code-quality suite. ACP concerns itself with boundaries, contracts, and structural drift — not formatting, style, or exhaustive linting, which existing tools already handle.
- **Not** a replacement for human judgment on semantics. It cannot tell whether a module does the *right* thing, only whether it stays within its declared shape and passes its declared contract.

---

## 3. Design principles

These are load-bearing. Every requirement descends from one of them, and any feature that violates one should be rejected.

1. **Truth is derived from code, never authored beside it.** The graph, the public-interface surface, and the structural metrics are all extracted from the source on every run. The only thing that can drift is the code violating a rule — and that is precisely what gets caught. There is no second model to keep in sync.
2. **The human authors only small things, and only at the boundary.** Intent statements, allowed-dependency lists, and boundary contract tests. If an artifact is large enough that reviewing it is as much work as reviewing the code, it has failed its purpose.
3. **Enforcement is deterministic; the LLM only advises.** Anything that blocks a merge must be a mechanical, reproducible static check. AI-derived judgments (naming, suspected smells) may inform and warn, but never gate, because they produce false positives and cannot be trusted to police the very output they resemble.
4. **The module boundary is the unit of trust.** Verification, encapsulation, and enforcement all attach to a module's public interface. Internals are trusted because the boundary is verified, not because anyone read them.
5. **Rigidity applies to accident, not intent.** Accidental structural change is blocked. Intentional structural change is cheap and first-class: the human edits the declaration, and that edit *is* the design decision, recorded and reviewable. The system must never make good refactoring expensive.
6. **The gate fails closed.** On ambiguity, missing declaration, or analysis failure, the default is to block, not to wave through. Silence is never treated as approval.
7. **Every stage stands alone.** The system is built and adopted in layers, and each layer delivers real control even if the next is never built.

---

## 4. Core concepts (domain model)

- **Module** — the unit of encapsulation and governance: a directory, package, or service with a boundary. Everything ACP does is defined per module.
- **Manifest** — the small, human-authored declaration attached to each module. Contains its intent, its public surface, and its allowed dependencies. The manifest is the *only* master artifact the human maintains.
- **Intent** — a short statement of the module's single responsibility and, importantly, what it must *not* do. The anchor against which responsibility drift is judged (largely by human/advisory review, not by the gate).
- **Facade / public surface** — the set of names a module deliberately exposes. Everything not in the facade is internal and unreachable from outside the module.
- **Allowed dependencies** — the explicit list of other modules this one may depend on, and in which direction. The absence of an entry is a prohibition.
- **Boundary contract test** — a human-owned behavioral test exercising a module through its facade only. The trust anchor that lets internals be ignored.
- **Derived model** — the actual structure extracted from code on each run: the real dependency graph, the real exported surface, and structural metrics/smell signals.
- **Reconciliation** — the comparison of the derived model against the manifests. Produces the set of violations.
- **The gate** — the policy engine that runs reconciliation and contract tests at a control point (pre-commit, pre-merge, CI) and returns pass or block.
- **Violation** — a specific, located, explained discrepancy: an illegal dependency, an undeclared export, a cycle, a failing contract, and so on.
- **Architecture diff** — the per-change delta in the shape of the system: edges added or removed, surface widened or narrowed, modules created or merged.

---

## 5. Components (the pieces)

Each component below lists its purpose, its responsibilities, its inputs and outputs, and the requirements specific to it. Functional and non-functional requirements are consolidated and enumerated in §6–§8.

### 5.1 Module Manifest

**Purpose.** Capture the intended shape of a single module in the smallest possible human-authored form.

**Responsibilities.** Declare the module's intent (responsibility and anti-responsibilities); declare its public surface; declare its allowed outbound dependencies and their direction; optionally declare structural invariants (e.g. layer, complexity ceilings, whether cycles are permitted internally).

**Inputs.** Authored and edited by the human architect. Editing a manifest is the sanctioned way to change the architecture.

**Outputs.** Consumed by the Reconciler and the Gate; rendered by the Visualization component.

**Requirements.** Must be small enough to read in under a minute. Must live alongside the code it governs (co-located), so it moves and versions with the module. Must be diffable in version control, so a change to intended architecture appears in history like any other change. Must degrade gracefully: a module without a manifest is reported as ungoverned rather than silently ignored.

### 5.2 Static Derivation Engine

**Purpose.** Extract the *actual* structure of the system from source, with no reliance on human-maintained metadata.

**Responsibilities.** Build the real dependency graph (which module actually imports/calls which). Extract each module's real exported surface (what is actually reachable from outside). Compute structural metrics and smell signals (cycles, complexity, clone/duplication candidates, pass-through/delegation ratios, cross-boundary access patterns, co-change coupling from version-control history).

**Inputs.** The source tree; version-control history (for co-change analysis).

**Outputs.** The derived model, consumed by the Reconciler and Reporting.

**Requirements.** Must be deterministic and reproducible for a given commit. Must be assembled primarily from existing ecosystem analyzers rather than built from scratch (see §10). Must be fast enough to run on every change without discouraging use. Must be honest about its own limits: where it cannot analyze reliably (dynamic dispatch, reflection, cross-language calls), it reports reduced confidence rather than a false clean result.

### 5.3 Reconciler

**Purpose.** Compare intended (manifests) against actual (derived model) and produce the authoritative list of violations.

**Responsibilities.** Detect dependencies that exist in code but are not permitted by any manifest. Detect exports that exist in code but are not declared in a facade (facade widening). Detect declared dependencies or exports that no longer exist (stale declarations). Detect cycles and direction/layer violations. Attach to each violation a precise location and a plain-language explanation of which rule was broken.

**Inputs.** Manifests and the derived model.

**Outputs.** A structured violation set, consumed by the Gate and Reporting.

**Requirements.** Every violation must be explainable in one sentence a human can act on. The reconciliation must be symmetric enough to catch both directions of drift: code exceeding its declaration, and declarations no longer matching code.

### 5.4 Boundary Contract Test Harness

**Purpose.** Turn "internals hidden" into "internals trusted" by verifying each module's behavior at its facade.

**Responsibilities.** Run the human-owned contract tests that exercise each module only through its public surface. Track which modules have contract coverage and which do not. Support property-based tests where the input space matters, to reduce the chance that hand-picked cases mask defects.

**Inputs.** Human-authored boundary tests; the module facades.

**Outputs.** Pass/fail per module, consumed by the Gate; coverage-at-boundary status, consumed by Reporting.

**Requirements.** Contract tests must reference only the facade, never internals — a test that reaches inside a module is itself a boundary violation and must be flagged. The harness must make it visible when a module's trust is unearned (internals hidden but no contract coverage), because such a module is a black box no one has verified. To mitigate correlated failure (the same misunderstanding producing both wrong internals and a wrong test that passes), the process must support authoring or reviewing contracts independently of the implementation pass (see §9).

### 5.5 The Gate

**Purpose.** The single decision point that converts everything above into a binary: this change may land, or it may not.

**Responsibilities.** Orchestrate derivation → reconciliation → contract tests at a control point. Apply the policy that determines which violation classes block and which only warn (see §7). Return a clear pass/block result with the reasons. Integrate at multiple control points: locally before commit, and authoritatively before merge in CI.

**Inputs.** The violation set; contract-test results; the enforcement policy.

**Outputs.** A pass/block decision plus a human-readable report.

**Requirements.** Must fail closed (§3.6). Must be fast enough at the local control point to be run habitually, and thorough at the CI control point where authority lives. Must be impossible to satisfy by editing generated code alone when the violation is structural — the only ways to clear a structural block are to fix the code or to deliberately change the manifest. Must never rely on an LLM judgment to block.

### 5.6 Reporting and Architecture Diff

**Purpose.** Restore the human's grip by summarizing each change as a shape delta rather than a wall of code.

**Responsibilities.** For each change, report what moved: edges added/removed, surface widened/narrowed, modules added/merged/split, new cycles, and any advisory smell signals crossing thresholds. Present blocked-change reports that name the exact rule and location. Distinguish clearly between hard violations and advisory signals.

**Inputs.** The derived model (before and after), the violation set, contract results.

**Outputs.** A per-change architecture diff and a violation report for human consumption.

**Requirements.** The diff must let the architect approve or reject a *structural* change in seconds without reading implementations. It must make an intentional structural change (a manifest edit) legible as such, so the reviewer sees "the architect widened this facade on purpose" rather than a mysterious new dependency.

### 5.7 Visualization (deferred)

**Purpose.** A read-only, navigable rendering of the real architecture and its per-change deltas.

**Responsibilities.** Render the derived graph with manifest overlays (allowed vs actual, violations highlighted). Render the architecture diff visually.

**Requirements.** Strictly read-only and strictly derived — it renders what the code and manifests say, and can never be edited to change them. This component is explicitly last and optional (§10); it must never become an authoring surface, because an editable authoritative diagram reintroduces the exact drift the whole system exists to prevent.

---

## 6. Functional requirements

| ID | Requirement | Priority |
|----|-------------|----------|
| FR-1 | The system shall extract the actual dependency graph, exported surface, and structural metrics from source on every governed change. | Must |
| FR-2 | The system shall allow each module to declare intent, public surface, and allowed dependencies in a co-located, version-controlled manifest. | Must |
| FR-3 | The system shall detect and report any code dependency not permitted by a manifest. | Must |
| FR-4 | The system shall detect and report any exported name not declared in the module's facade. | Must |
| FR-5 | The system shall detect and report dependency cycles and violations of declared direction/layering. | Must |
| FR-6 | The system shall detect stale declarations (declared dependencies or exports absent from the code). | Must |
| FR-7 | The system shall run human-owned boundary contract tests through module facades and report pass/fail per module. | Must |
| FR-8 | The system shall flag any test or external caller that reaches a module's internals rather than its facade. | Must |
| FR-9 | The system shall block a change at the gate when any hard-violation class is present, and fail closed on analysis errors or missing declarations. | Must |
| FR-10 | The system shall produce, per change, an architecture diff describing how the system's shape moved. | Must |
| FR-11 | The system shall let the architect change intended architecture by editing a manifest, recording that edit as an auditable design decision. | Must |
| FR-12 | The system shall report modules that hide internals but lack boundary contract coverage as unverified. | Should |
| FR-13 | The system shall surface advisory structural signals (duplication, co-change coupling, delegation ratio, message chains, speculative generality) without blocking on them. | Should |
| FR-14 | The system shall report ungoverned modules (no manifest) distinctly from governed ones. | Should |
| FR-15 | The system shall support importing an existing codebase by generating draft manifests from the current derived structure for human ratification. | Should |
| FR-16 | The system shall provide an LLM-assisted advisory pass for judgment-based smells (e.g. unclear names), clearly marked as non-blocking. | Could |
| FR-17 | The system shall render a read-only visual of the architecture and its deltas. | Could |

## 7. Enforcement policy — what blocks vs what warns

A defining design decision: enforcement is **tiered**, so that only deterministic, low-false-positive checks ever block a merge. This directly implements §3.3.

**Tier A — Hard gates (deterministic; block the merge):** illegal dependency; undeclared export / facade widening; dependency cycle; direction/layer violation; a caller or test reaching module internals; failing boundary contract test; fail-closed conditions (missing manifest on a governed module, analysis error).

**Tier B — Advisory signals (deterministic; reported, do not block by default):** cross-module code duplication; co-change coupling hotspots (proxy for shotgun surgery / divergent change); excessive delegation / pass-through (proxy for middle man); message chains; single-implementor abstractions and unused extension points (proxy for speculative generality); complexity threshold breaches. Any of these *may* be promoted to a hard gate per project, but the default is warn-and-record so the architect decides.

**Tier C — Judgment-assisted (LLM or human; advisory only, never blocking):** unclear/mysterious names; suspected data clumps and primitive obsession; suspected feature envy where intent is ambiguous. These carry false-positive risk and concern semantics the tool cannot verify, so they are surfaced for human attention and never gate.

## 8. Non-functional requirements

| ID | Requirement |
|----|-------------|
| NFR-1 | **Determinism.** For a given commit, derivation and reconciliation produce identical results every run. |
| NFR-2 | **Zero drift by construction.** No component maintains a copy of structure that could diverge from the code; all structural truth is derived. |
| NFR-3 | **Low authoring overhead.** Human-maintained artifacts (manifests, contracts) stay small enough to review in seconds to a minute. |
| NFR-4 | **Speed.** The local gate is fast enough to run on every commit; the CI gate is authoritative and may be more thorough. |
| NFR-5 | **Explainability.** Every blocking decision names the exact rule, location, and remedy in one plain sentence. |
| NFR-6 | **Fail-closed safety.** Ambiguity, missing declarations, or analysis failure default to block. |
| NFR-7 | **Incremental adoptability.** The system delivers control at stage one and can be added to an existing codebase module by module. |
| NFR-8 | **Tooling reuse.** Derivation and enforcement build on existing ecosystem analyzers wherever possible, not bespoke reimplementations. |
| NFR-9 | **Honest confidence.** Where static analysis cannot see reliably, the system reports reduced confidence rather than a false clean result. |
| NFR-10 | **Non-authoritative visualization.** Any visual is read-only and derived; it can never be edited to change intended architecture. |

---

## 9. Trust model

The central claim of this system is that a module's internals can be safely ignored. That claim has to be earned, and tests alone do not earn it: an agent that misunderstands a requirement will tend to write both the wrong implementation and a passing test encoding the same misunderstanding (correlated failure). ACP addresses this with three layers.

First, **the boundary is the contract.** Trust attaches to behavior observed through the facade, not to implementation. This keeps the human's verification surface small — a facade contract is far smaller than the code behind it — which is what makes reviewing it, rather than the internals, a real reduction in work.

Second, **independence breaks correlation.** The system supports authoring or reviewing a module's contract in a pass separate from the implementation pass, and encourages property-based tests that explore inputs the implementer did not hand-pick. The more the contract's origin is independent of the implementation's, the less likely a shared blind spot survives.

Third, **unearned trust is made visible.** A module that hides internals but has no boundary contract coverage is reported as unverified. The system never lets encapsulation masquerade as verification; hiding something is not the same as trusting it.

What the human still owns, irreducibly: deciding whether a module's declared *intent* is the right intent, and whether its facade contract expresses the right behavior. ACP guarantees the module stays inside its declared shape and passes its declared contract. It cannot guarantee the shape and the contract were the right ones to declare — that judgment is the architect's job and the reason the human remains in the loop.

---

## 10. Adoption and build roadmap

The system is built in stages, each independently useful. Do not build a later stage until the earlier ones are load-bearing.

**Stage 1 — Boundary gate.** Manifests (intent, allowed dependencies, declared surface) plus a gate that fails on illegal dependencies, cycles, and facade widening. This alone delivers most of the control: the agent physically cannot couple separated modules or quietly widen an interface without a red build. Highest control-per-effort; relies almost entirely on existing dependency-analysis tooling plus a small reconciler. Ship this first even if nothing else follows.

**Stage 2 — Contract trust anchor.** Human-owned boundary contract tests, coverage-at-boundary reporting, and the independence practices of §9. This is what converts "internals hidden" into "internals trusted" and makes the black-box model honest.

**Stage 3 — Architecture diff and (optionally) visualization.** Per-change shape deltas so the architect reviews structure in seconds, and — only after stages 1–2 are solid — a strictly read-only visual. Stop here. The moment a visual becomes an editable master, drift returns.

Across all stages, the custom code to be written is small: chiefly the manifest format, the reconciler, and the reporting/diff. Derivation, testing, duplication/complexity analysis, and the coding itself are assembled from tools that already exist.

---

## 11. Risks, limitations, and open questions

### 11.1 Scope creep toward a studio
The strongest pull on this system is to grow a visual authoring surface where the diagram becomes the thing you edit. That single step reintroduces the drift the system exists to eliminate. Treat §2.2 and §3.1 as non-negotiable.

### 11.2 Boundary rigidity vs emergent design
Good interfaces are rarely right the first time; they are discovered by building and refactoring. A system that makes boundary changes painful would ossify premature abstraction — itself a smell. The mitigation is §3.5: intentional change is a cheap, first-class manifest edit. This must be genuinely cheap in practice, or the tool will be resented and bypassed. Watch this closely in real use.

### 11.3 Authoring overhead
If manifests or contracts grow large, the human is back to reviewing as much as they would have anyway, and adoption collapses. Smallness is a hard requirement, not a nicety (NFR-3). Prefer generating draft manifests from the derived structure and having the human ratify, rather than authoring from scratch.

### 11.4 Semantic drift is invisible to the tool
ACP verifies structure and boundary behavior. It cannot see whether a module quietly started doing the *wrong* thing while staying inside its shape and passing its contract. Intent statements and Tier C advisories help a human notice, but this class of drift remains a human responsibility.

### 11.5 Analysis blind spots
Dynamic dispatch, reflection, metaprogramming, and cross-language or cross-service calls can hide real dependencies from static derivation. The system must report these as reduced-confidence zones (NFR-9) rather than imply a clean result it cannot back up.

### 11.6 Brownfield reality
Existing codebases will produce large violation sets on first import. The system needs a ratify-and-baseline path (accept current reality as the starting declaration, then govern change from there) or adoption stalls under an unfixable initial backlog.

### 11.7 Open questions
- What is the right granularity of a "module" across different stacks (directory, package, service), and how does that choice interact with monorepo vs multi-repo layouts?
- How are cross-module contracts (integration behavior spanning two facades) owned and verified without pulling internals into scope?
- When advisory signals (Tier B) are promoted to hard gates, how is the increased false-positive cost managed?
- How is the manifest kept authoritative for *intent* when intent is inherently semantic and only partially machine-checkable?

---

## 12. Success criteria

The system is working if: no dependency, cycle, or facade change reaches the main branch without either matching a manifest or being an explicit, recorded manifest edit; the architect reviews most changes by reading the architecture diff rather than the implementation; modules with hidden internals reliably carry boundary contracts, and unverified ones are visibly flagged; and evolving the architecture on purpose is a quick manifest edit rather than a fight with the tool. It is failing if manifests grow large, if the team routinely bypasses the gate, or if a visual layer has quietly become the place people edit the design.

---

## 13. Glossary

**ACP** — Architecture Control Plane, this system.
**Facade / public surface** — the deliberately exposed names of a module; everything else is internal.
**Manifest** — the small, co-located, human-authored declaration of a module's intent, surface, and allowed dependencies.
**Derived model** — actual structure extracted from source on each run.
**Reconciliation** — comparison of derived model against manifests, producing violations.
**Boundary contract test** — a human-owned test exercising a module only through its facade.
**Architecture diff** — the per-change delta in the system's structure.
**Gate** — the deterministic pass/block decision point.
**Tier A / B / C** — hard-blocking / advisory-deterministic / judgment-assisted enforcement classes.
**Correlated failure** — the same misunderstanding producing both a wrong implementation and a wrong test that passes it.
**Fail closed** — defaulting to block on ambiguity or error.
