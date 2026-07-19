# 05 · Phase M2 — Behavior Plane (sketch)

**Why here.** Closes the architecture plane's **semantic blind spot**: a rewrite can keep the shape legal and tests green while quietly changing what the system *does* (PRD §7.3, §13). Built after M1 so the tests it relies on are already verified.

**Governs.** What the system actually does at its boundaries.

**Human declares.** Nothing up front — the human **ratifies deltas** (the defining ergonomic of this plane). Optional `behavior:` section marks which boundaries to pin.

**Grip derives.** Characterization / approval snapshots of observed boundary behavior captured from **real runs**.

**Blocks (Tier A).** A change that alters pinned boundary behavior without an accompanying ratification (GR-BEH-1).
**Warns (Tier B).** Newly observed behaviors not yet pinned; behavior in modules with no snapshot (GR-BEH-2).

---

## Fit to the plane contract

Same `Plane` interface. Stresses a **third kind of derived model**: recorded I/O snapshots + a baseline, plus a **ratify** workflow (reuses `internal/ratify` from M0.10).

- **Deriver:** capture boundary behavior by exercising the module facade — from existing test runs and/or recorded fixtures — into deterministic snapshots (approval-test style). Wrap existing approval/snapshot tooling where it exists; otherwise a thin capture harness invoked through the facade only (never internals — reuses the M0 internal-reach rule to keep tests honest).
- **Reconcile (pure):** derived snapshot ≠ pinned snapshot **and** no ratification in the diff → `behavior.unratified-change`. Snapshot == pinned → pass. New boundary, no pin → Tier B.
- **Ratify:** `grip ratify behavior <module>` re-pins the current snapshot; the ratification is the recorded design decision (principle 5), rendered as intentional in the shape diff.

## The hard problem: cheap, stable capture (PRD §16 open question)
- **Flakiness/nondeterminism** is the enemy. Snapshots must be normalized (strip timestamps, ordering, addresses) and captured from deterministic runs. Non-deterministic boundaries are reported as `reduced` confidence (NFR-9), not silently pinned.
- **Cheapness:** piggyback on existing tests rather than heavy instrumentation; only pin boundaries the human opts into, keeping the surface small (NFR-3).

## Exit criteria (unlocks M3)
On the fixtures: an internal rewrite that changes observable boundary output is blocked pending ratification; a behavior-preserving rewrite passes; ratify re-pins cleanly and shows as intentional; flaky/nondeterministic boundaries degrade to reduced-confidence rather than false pins.
