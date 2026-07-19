# Grip — Implementation Plan · Overview

**Status:** Plan v1 · **Companion inputs:** `reqs/grip-prd.md`, `reqs/architecture-control-plane-spec.md`

This folder is the build plan for **Grip**, a deterministic control plane that lets an engineer stay the architect while AI agents implement — declaring small intent at boundaries, deriving real state from code, and gating any change that moves the system's shape without consent.

The plan is **deep for M0** (the shared engine + Architecture plane) and **sketched for M1–M4** (test-rigor, behavior, contract, advisories/visualization). This matches the PRD's release discipline: *do not build a later stage until earlier ones are load-bearing* (PRD §13, ACP §10).

---

## 1. How to read this folder

| File | What it covers | Depth |
|------|----------------|-------|
| `00-overview.md` | This file — north star, decisions log, phase map, glossary pointer | — |
| `01-architecture-and-tech-decisions.md` | Go engine internals, deriver-wrapping strategy, the common graph IR, manifest + config schemas, Grip's own repo layout | Deep |
| `02-plane-plugin-contract.md` | The interface every plane implements (schema + deriver + rules). The keystone that keeps the engine plane-agnostic | Deep |
| `03-phase-M0-engine-and-architecture-plane.md` | The MVP, broken into sub-phases M0.0–M0.11 with tasks, deliverables, and exit criteria | Deep |
| `04-phase-M1-test-rigor-plane.md` | Mutation-based verification, skip/delete/threshold-tamper detection | Sketch |
| `05-phase-M2-behavior-plane.md` | Boundary snapshotting, ratify-on-delta | Sketch |
| `06-phase-M3-contract-plane.md` | API/event/schema/migration compatibility gating | Sketch |
| `07-phase-M4-advisories-and-visualization.md` | Tier B/C advisory passes + read-only visual | Sketch |
| `08-testing-and-acceptance.md` | Validation strategy, fixture repos, FR/NFR → test mapping | Deep |
| `09-risks-open-questions-and-sequencing.md` | Risk register, open questions with plan-level answers, milestone dependency graph | Deep |

**Read order for building:** 01 → 02 → 03, using 08 as the acceptance harness. 04–07 are read when their milestone is next; 09 is the standing reference.

---

## 2. North star (non-negotiable principles)

Copied from PRD §6 / ACP §3 because every task in this plan descends from one of them. A task that violates one is wrong.

1. **Truth is derived from code, never authored beside it.** No component keeps a copy of state that can diverge from source.
2. **The human authors only small things, and only at the boundary.** If a manifest is as much work to review as the code, it has failed.
3. **Enforcement is deterministic; the LLM only advises.** Anything that blocks a merge is a mechanical, reproducible check.
4. **The boundary is the unit of trust.** Verification attaches to public interfaces; internals are trusted because boundaries are verified.
5. **Rigidity applies to accident, not intent.** Accidental drift is blocked; intentional change is a cheap, recorded manifest edit.
6. **The gate fails closed.** Ambiguity, missing declaration, or analysis failure defaults to block.
7. **Every plane and stage stands alone.** Each delivers real control even if the next is never built.
8. **One gate, one review surface, one manifest home.**

---

## 3. Locked decisions (the plan's premises)

These were agreed during planning and are treated as fixed inputs. Changing one invalidates parts of the plan; the affected files note their dependence.

| # | Decision | Consequence in the plan |
|---|----------|-------------------------|
| D1 | **Engine + CLI written in Go**, distributed as a single static binary | Derivers run as subprocesses; concurrency via goroutines; cobra-style CLI |
| D2 | **M0 governs PHP and TypeScript/JS simultaneously** | The deriver layer and IR are multi-language from day one — not retrofitted |
| D3 | **Derivers wrap existing tools** (deptrac/PHPStan; dependency-cruiser/ts-morph), normalized into one common graph IR | Grip owns the IR + reconciler + gate; it does not reimplement language parsers in M0 |
| D4 | **Manifest = YAML, co-located, directory-based modules** | A module is a directory containing `grip.yaml`; graph nodes are directories |
| D5 | **Repo-level `.grip.yaml` config** selects enabled planes and tier promotions | Engine reads config first; unknown planes fail closed |
| D6 | **Distribution:** local CLI + GitHub Actions + GitLab CI; **GPL-3.0** | Two control points (pre-commit fast, CI authoritative); reusable CI actions shipped |
| D7 | **Deep M0, sketch M1–M4** | Only M0 has task-level detail; later planes prove the plugin contract holds |
| D8 | **No real dogfood repo yet** → synthetic PHP+TS fixture repos are the acceptance harness | Fixtures are a first-class M0 deliverable (M0.11), not an afterthought |
| D9 | **Future stacks:** Python, Java/Kotlin, Go, C# | The plugin contract (file 02) must not encode PHP/TS assumptions into the engine |

---

## 4. The one repeated loop

Everything Grip does, every plane, is the same loop. The engine implements it once; planes fill in the verbs.

```
Declare (manifest)  →  Derive (from code)  →  Reconcile (intent vs actual)  →  Gate (tiered, fail-closed)  →  Diff/Report (shape, not lines)
```

The engine owns Reconcile, Gate, and Diff/Report generically. Each plane supplies its **manifest schema**, its **deriver**, and its **tiered rule set**. See `02-plane-plugin-contract.md`.

---

## 5. Milestone map (one line each)

- **M0 — Engine skeleton + Architecture plane (MVP).** Manifest/config, common IR, PHP+TS derivers, reconciler, tiered gate, shape diff, CLI, pre-commit + CI, onboarding/ratify. *Delivers most of the control on its own.*
- **M1 — Test-rigor plane.** Mutation-based verification + tamper detection. *Built second because it makes every other plane's trust real.*
- **M2 — Behavior plane.** Boundary snapshotting, ratify-on-delta. *Closes the semantic blind spot.*
- **M3 — Contract plane.** API/event/schema/migration compat gating.
- **M4 — Advisories + read-only visualization.** Tier B/C passes + read-only visual. *Last, and optional. Stop before it becomes a studio.*

Sequencing rationale and the exit criteria that unlock each next milestone live in `09-risks-open-questions-and-sequencing.md`.

---

## 6. Glossary

The PRD §18 and ACP §13 glossaries are authoritative. Key terms used throughout the plan: **Module** (directory with a `grip.yaml`), **Manifest** (per-module declaration), **Facade** (declared public surface), **Derived model / IR** (actual state extracted per run), **Reconciliation** (declared vs derived → violations), **Gate** (single fail-closed pass/block), **Shape diff** (per-change delta across planes), **Tier A/B/C** (hard-block / advisory-deterministic / judgment-assisted), **Ratify/baseline** (accept current reality as the declared start).
