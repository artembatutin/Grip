# 02 · The Plane Plugin Contract

The keystone of the whole product thesis: *"Grip is one engine and four policy planes, not four tools"* (PRD §3.2, §7.1 GR-ENG-6, ACP is Plane 1 realized). This file specifies the interface a plane implements so that **new planes plug in without touching the engine**.

This contract is designed and implemented in M0 even though only the Architecture plane exists — because the only way to know the seam is right is to build it before the second plane needs it (M1). If the engine ever grows a `switch plane { case architecture … }` in its core loop, the contract has failed.

---

## 1. What a plane is

A plane governs exactly one axis of drift. It contributes **three things and nothing else**:

1. A **manifest schema** — the YAML section it owns under a module's `grip.yaml` (and any repo-level config under `.grip.yaml`).
2. A **deriver** — extracts this plane's actual state from code into a plane-scoped model.
3. A **tiered rule set** — pure functions from `(declared, derived)` to violations, each tagged Tier A/B/C.

The engine owns everything else: module discovery, config, orchestration, the gate decision, diff, reporting, exit codes, fail-closed policy.

---

## 2. The Go interface (M0 target shape)

Illustrative — names may refine during M0.2, but the *shape* is the commitment.

```go
package plane

// Plane is the extension point. One value per governed axis.
type Plane interface {
    // Identity
    ID() string                    // "architecture", "test-rigor", …
    ManifestSection() string       // top-level key it owns in grip.yaml

    // Declare: parse & validate this plane's slice of a module manifest.
    // Returns a plane-specific Intent, or a fail-closed error.
    ParseIntent(raw ManifestSection, mod ModuleRef) (Intent, error)

    // Derive: produce this plane's actual-state model for the given modules.
    // May shell out to external tools via the provided Deriver services.
    Derive(ctx Context, mods []ModuleRef, svc DeriveServices) (Derived, error)

    // Reconcile: pure comparison. NO I/O. Deterministic. The heart of the plane.
    Reconcile(intent Intent, derived Derived) []Violation

    // Rules: static description of every rule, its tier, and its default.
    // Used by config validation (tier promotion) and docs generation.
    Rules() []RuleSpec
}
```

Supporting types the engine defines and owns:

```go
type Violation struct {
    RuleID     string     // stable id, e.g. "arch.illegal-dependency"
    Tier       Tier       // A (block) | B (advisory) | C (judgment)
    Location   Location   // file:line, module id
    Message    string     // ONE plain sentence: rule + what + remedy (NFR-5)
    Confidence Confidence // full | reduced | none  (NFR-9)
    Kind       Kind       // violation | staleDeclaration | intentionalChange
}

type RuleSpec struct {
    ID          string
    Tier        Tier      // default tier
    Promotable  bool      // may a repo promote B→A? (PRD §9)
    Summary     string
}

type Tier int  // TierA, TierB, TierC
```

**Hard constraints on any implementation:**
- `Reconcile` is **pure and deterministic** — no clock, no filesystem, no network, no map-iteration-order leaks. This is what makes NFR-1 (determinism) and principle 3 (deterministic enforcement) hold structurally.
- All I/O and tool invocation happens in `Derive`, behind `DeriveServices` (so it can be mocked/recorded in tests).
- Tier C rules may consult an LLM *inside `Derive`* to produce advisory signals, but they **emit only Tier C violations** and the gate never blocks on them (principle 3). The engine enforces this: a Tier C violation cannot change the exit code.

---

## 3. The gate's generic loop (engine-owned, plane-agnostic)

```
for each enabled plane P (from .grip.yaml):
    intents  = P.ParseIntent(manifest section) for each governed module   # fail-closed on error
    derived  = P.Derive(modules)                                          # fail-closed on tool error
    violations += P.Reconcile(intents, derived)

decision = block if ANY TierA violation OR ANY fail-closed condition
           else pass
report = render(all violations, grouped by tier, with shape diff)
exit(codeFrom(decision))
```

The loop mentions no plane by name. `ANY TierA` across all planes is the "one gate — a change clears all planes or it does not land" promise (PRD §3.2). Fail-closed conditions (missing manifest on governed module, `Derive` error, `reduced`-confidence touching a rule) are engine-level and apply uniformly.

---

## 4. Why this design satisfies the requirements

| Requirement | How the contract satisfies it |
|-------------|-------------------------------|
| GR-ENG-6 (plugin contract; add planes without engine changes) | The three-method interface + engine-owned loop. New plane = new `Plane` registered in the registry. |
| Principle 8 (one gate, one review surface, one manifest home) | Single loop aggregates all planes' violations; one report; sections co-located in one `grip.yaml`. |
| Principle 3 (deterministic; LLM only advises) | `Reconcile` is pure; Tier C is structurally non-blocking. |
| Principle 7 (every plane stands alone) | Planes are independent registrations; disabling one in config removes it cleanly. |
| NFR-11 (language/stack adaptability) | Derivers are per-stack and live *inside* a plane's `Derive`; the interface is language-neutral. |

---

## 5. The Architecture plane as the reference implementation (M0)

M0 builds `internal/plane/architecture` as the first — and, until M1, only — implementation of this interface. It proves the contract concretely:

- **ManifestSection:** `architecture` (facade, dependencies, cycles, layer).
- **Derive:** runs the language derivers (file 01 §3–4) and produces the Common Graph IR as its `Derived` model.
- **Reconcile (pure):** illegal dependency, facade widening, cycle, direction/layer violation, internal-reach, stale declaration — the FR-3…FR-8 rule set.
- **Rules():** the Tier A set above, plus Tier B advisories (duplication, co-change, delegation, message chains, speculative generality) declared but mostly deferred to M4 for full implementation; declaring them now lets `.grip.yaml` promotion validate against real rule ids.

Full mechanics of these rules are in `03-phase-M0-engine-and-architecture-plane.md` §M0.5.

---

## 6. Anti-goals for the contract

- **No shared mutable derived state between planes.** If M2's behavior plane wants the arch graph, it re-derives or reads the IR read-only; planes never write each other's models.
- **No plane ordering dependencies for correctness.** Planes may run in any order and concurrently; the gate result is the union of their violations. (Reporting may present them in a stable, human-friendly order — that's cosmetic.)
- **No engine knowledge of any specific plane.** Verified by a test that greps the engine core for plane ids and fails if any leak outside the registry/config layer.
