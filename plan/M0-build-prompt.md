# M0 Build Prompt — Grip Engine + Architecture Plane

> Hand this to an implementing agent (or use it yourself) to build M0. It is self-contained on intent and constraints, and points to the `plan/` files for full detail. Paste it as the opening instruction of a fresh build session run from the repo root.

---

## Role & mission

You are implementing **M0 of Grip** — a deterministic control plane, written in **Go**, that gates AI-agent code changes so a human stays the architect. M0 is the MVP: **the shared engine + the Architecture plane**, governing **PHP and TypeScript/JS** codebases.

The one-sentence goal: *an agent physically cannot introduce an illegal dependency, widen a module's facade, create a dependency cycle, reach into another module's internals, or violate layer direction — in PHP or TS — without a red build; and the human reviews the shape delta, not the lines.*

M0 is **done** when the acceptance harness (below) is green: every scripted "bad agent" diff is blocked with a correct one-sentence report, every "good" diff passes, intentional manifest edits pass and render as intentional, the CI integration blocks a real PR, and derivation is bit-for-bit deterministic across runs and machines.

## Required reading (read before writing any code, in this order)

1. `plan/00-overview.md` — principles, locked decisions, phase map.
2. `plan/01-architecture-and-tech-decisions.md` — engine internals, repo layout, the **Common Graph IR**, manifest + config schemas, tool-wrapping list, concurrency, exit codes.
3. `plan/02-plane-plugin-contract.md` — the **Plane interface** and generic gate loop. This is the keystone.
4. `plan/03-phase-M0-engine-and-architecture-plane.md` — the sub-phase build order M0.0→M0.11 with per-step exit criteria. **This is your task list.**
5. `plan/08-testing-and-acceptance.md` — fixtures, acceptance matrix, requirement→test traceability.
6. `plan/09-…-sequencing.md` — risk register and the M0 exit gate.

`reqs/grip-prd.md` and `reqs/architecture-control-plane-spec.md` are the authoritative source of intent behind all of the above; consult them when a requirement ID (FR-x, GR-x, NFR-x) needs grounding.

---

## Non-negotiable constraints

These are load-bearing. Any code that violates one is wrong, no matter how convenient.

**Principles (from PRD §6 / ACP §3):**
1. Truth is **derived from code**, never authored beside it — no component keeps a second copy of state that can drift.
2. The human authors only **small** things, only at the boundary (manifests read in under a minute).
3. Enforcement is **deterministic; the LLM only advises** — nothing that blocks a merge may depend on an LLM. (No LLM anywhere in M0.)
4. The **boundary is the unit of trust** — verification attaches to a module's public surface.
5. **Rigidity applies to accident, not intent** — an intentional manifest edit is cheap, first-class, and rendered as intentional, never as a mystery violation.
6. The gate **fails closed** — ambiguity, missing declaration, missing tool, or analysis error defaults to **block**. Silence is never approval.
7. Every stage stands alone.
8. One gate, one review surface, one manifest home.

**Locked decisions (do not relitigate — see `00-overview.md` D1–D9):**
- Go single static binary; cobra CLI; `errgroup` concurrency; `golangci-lint`. **GPL-3.0.**
- M0 governs **PHP and TS simultaneously** — the deriver layer and IR are multi-language from day one, not retrofitted.
- Derivers **wrap existing tools** and normalize to one IR: TS → `dependency-cruiser` (+ a bundled `ts-morph` helper for exported surface); PHP → `deptrac` (+ PHPStan / `nikic/php-parser` helper). **Do not parse PHP or TS natively in M0.**
- Manifest = **YAML**, co-located, **directory-based modules** (a directory with `grip.yaml` is a module; graph nodes are directories).
- Repo config = `.grip.yaml` at root (enables planes, tier promotions).
- Two control points: local pre-commit (fast, incremental) and CI (authoritative, full). Ship a GitHub Action **and** a GitLab template.

**Two structural guarantees you must enforce in the design, not just by convention:**
- **Determinism (NFR-1):** identical commit + tool versions ⇒ byte-identical IR and identical gate decision, across runs and machines. Canonically sort everything before hashing/output; no timestamps or absolute paths in the IR; capture resolved analyzer versions in the report. The IR hash is asserted in CI.
- **Fail-closed (NFR-6):** missing manifest on a governed module referenced by a rule, deriver/tool error, missing tool for an enabled language, reduced/none analysis confidence touching a rule, unknown enabled plane, malformed config → **block** with a distinct, actionable reason. Exit code `2` for fail-closed vs `1` for a real hard violation.

---

## Lock these two artifacts first (hardest to change later)

Before writing any deriver, freeze and test:

1. **The Common Graph IR** (`internal/ir`, schema in `01 §4`): language-neutral modules + directed module-level edges (with `file:line:symbol` evidence) + per-module exported/reachable surface + per-scope confidence records. Canonical sort + content hash + `irVersion`. Golden-file round-trip test.
2. **The Plane interface** (`internal/plane`, shape in `02 §2`): `ID`, `ManifestSection`, `ParseIntent`, `Derive`, `Reconcile` (pure — no I/O, deterministic), `Rules`. Plus `Violation`/`RuleSpec`/`Tier` types and a `Registry`.

Then write the **engine-core-purity test**: it fails if any plane id (`"architecture"`, etc.) appears in engine-core packages outside `internal/plane/registry` and `internal/config`. This mechanically guarantees a second plane (M1) plugs in without touching the engine.

---

## Build order (follow `plan/03`, M0.0 → M0.11)

Proceed in dependency order; several overlap once the IR (M0.2) lands. For each sub-phase, satisfy its **exit criteria** in `plan/03` before moving on.

| Step | Deliverable | Done when |
|------|-------------|-----------|
| **M0.0** | Go scaffold, layout from `01 §2`, GPL-3.0, own CI, `make check` | binary builds static; `grip --version`; lint+test green |
| **M0.1** | Manifest + `.grip.yaml` loaders; module discovery (governed vs ungoverned) | malformed → fail-closed with precise message; discovery deterministic |
| **M0.2** | **Common Graph IR + Plane interface + Registry + purity test** | IR hashes identically 100×; purity test green |
| **M0.3** | TS deriver (wrap dependency-cruiser + ts-morph) | golden IR matches hand-verified fixture; deterministic |
| **M0.4** | PHP deriver (wrap deptrac + PHPStan helper) — **same IR schema** | mixed PHP+TS fixture merges into one IR; no engine language-branching outside `internal/derive` |
| **M0.5** | Reconciler + Architecture Tier A rules | every rule fires correctly **and only** correctly; reconcile is pure |
| **M0.6** | Gate: tiered policy, fail-closed, local/CI modes, exit codes | every fail-closed condition blocks distinctly; promoted advisory blocks |
| **M0.7** | Shape diff + report (human/JSON/SARIF) | diff names exactly what moved; manifest-only edit renders as intentional and passes |
| **M0.8** | CLI (`gate`, `derive`, `diff`, `modules`, `ratify`, `init`, `version`) | each command hits the intended engine path; `--plane` runs one plane |
| **M0.9** | Pre-commit hook + GitHub Action + GitLab template | a bad PR/MR is blocked with an inline annotation; clean passes |
| **M0.10** | Generate-then-ratify (`grip init` / `grip ratify`) | fresh repo → green gate in one sitting; drafts small |
| **M0.11** | Fixture repos + acceptance harness (`make acceptance`) | **the M0 gate** — full matrix green (see below) |

The **Architecture plane Tier A rule set** (M0.5), each producing a located, one-sentence message with a remedy (NFR-5):
`arch.illegal-dependency` (FR-3) · `arch.facade-widening` (FR-4) · `arch.cycle` (FR-5, Tarjan on the IR — Grip's own, not delegated) · `arch.direction-violation` (FR-5) · `arch.internal-reach` (FR-8) · `arch.stale-declaration` (FR-6, symmetric drift). A rule whose evidence lands in a reduced/none-confidence scope emits a **"cannot verify — blocked"** variant.

---

## Testing requirements (build alongside, not after — see `plan/08`)

- **Pure reconciler + gate policy** get exhaustive table-driven unit tests; they decide pass/block and must be hermetic and deterministic.
- **Derivers** are tested against **recorded analyzer output** (golden IR), so the suite is fast and offline.
- **Fixture repo** (`testdata/fixtures/`): one clean multi-module **PHP + TS** repo with a declared layer order, one ungoverned module, one reduced-confidence spot — plus a library of scripted diffs.
- **Acceptance matrix** (`make acceptance`): every "bad agent" diff must **block** with the exact expected rule + `file:line` + remedy string; every "good" diff must **pass**; every intentional manifest edit must pass and render as intentional. Assert exact report strings via golden files.
- **Both-directions coverage:** every rule needs a positive fixture (fires) *and* a negative near-miss (does not fire), to bound the false-block rate.
- **Determinism CI matrix** (Linux + macOS): identical IR hash and gate decision.
- **Dogfood:** mutation-test Grip's own reconciler once it exists — a surviving mutant means the fixtures are too weak.

---

## Guardrails — do NOT build these in M0

- **No LLM** anywhere in the gate path (Tier C is M4, advisory only).
- **No native PHP/TS parsing** — wrap the existing tools and normalize.
- **No visualization / graph UI** (M4, read-only).
- **No mutation, behavior, or contract logic** (M1–M3).
- **No auto-fix / remediation** (out of scope — Grip blocks and explains).
- **No engine coupling to any specific plane** — if you're about to write `switch plane` in engine core, stop; the seam is wrong.
- Don't gold-plate: the custom surface is deliberately small (manifest format, reconciler, plane contract, diff/report). Push language complexity into the wrapped tools.

---

## Working method

- Work in **small, tested increments**; run `make check` (build + vet + lint + test) before each commit. Don't push or commit unless asked, and branch off `main` first if you do.
- **Report progress against the M0 exit checklist**, not as prose — say which boxes are green.
- **Resolve small ambiguities with sensible defaults and note them** (e.g., nearest-ancestor `grip.yaml` owns a nested file; cobra command naming). **Stop and ask** only when a choice changes gate *semantics* or a locked decision.
- When you wire the reconciler, remember the failure mode that matters most: **a false pass is the worst bug** — it silently returns the drift Grip exists to prevent. Prefer fail-closed and a redundant test over cleverness.
- Keep messages to **one plain, actionable sentence** naming rule, location, and remedy (NFR-5). User-facing strings are golden files — changing one is a visible, reviewed diff.

---

## Definition of done (M0 exit checklist — from `plan/03`)

- [ ] PHP **and** TS derive into one IR; no engine branch on language outside `internal/derive`.
- [ ] All Tier A architecture rules fire correctly, and only correctly, on fixtures (positive + negative each).
- [ ] Gate fails closed on every defined ambiguity/error condition, with distinct exit codes (`0`/`1`/`2`/`3`).
- [ ] Shape diff renders intentional vs accidental change correctly.
- [ ] CLI + pre-commit + GitHub Action + GitLab template each block a bad change and pass a clean one.
- [ ] Generate-then-ratify brings a fresh repo to green in one sitting, with small drafts.
- [ ] Determinism (IR hash) holds across runs and machines (CI matrix).
- [ ] Plane contract proven by the Architecture plane with zero engine coupling — ready for a second plane.

## First actions

1. Read the six plan files above.
2. Scaffold M0.0 (Go module, layout, GPL-3.0, `make check`, own CI).
3. Stand up the clean fixture base repo (PHP + TS) in parallel — you need it to test everything else.
4. Lock the IR schema (`01 §4`) and the Plane interface (`02 §2`) with golden + purity tests **before** writing any deriver.
5. Then proceed M0.3 → M0.11, keeping the acceptance matrix green as it grows.
