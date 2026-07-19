# 03 · Phase M0 — Engine Skeleton + Architecture Plane (MVP)

**Goal:** the smallest thing that delivers real grip — the agent physically cannot couple separated modules, widen a facade, or introduce a cycle in PHP or TS without a red build, and the human reviews the shape delta, not the lines.

**Definition of done for M0:** on the fixture repos (file 08), a set of scripted "bad" agent changes are each blocked with a correct, one-sentence report; a set of "good" changes pass; intentional manifest edits pass and render as intentional; the CI action blocks a real PR; determinism (identical IR hash across runs) holds. Maps to FR-1…FR-15 (Must subset), GR-ENG-1…9, GR-ARC-1, GR-X-1…5, NFR-1…9.

M0 is decomposed into sub-phases **M0.0 → M0.11**. Each has: intent, tasks, deliverables, exit criteria. They are ordered by dependency; several can overlap once M0.2 (the IR) lands.

---

## Dependency order

```
M0.0 scaffold
   └─▶ M0.1 manifest+config ──▶ M0.2 IR + plane contract ──┬─▶ M0.3 TS deriver ──┐
                                                            └─▶ M0.4 PHP deriver ─┤
                                                                                  ▼
                                          M0.5 reconciler (arch rules) ◀──────────┘
                                                    │
                                                    ▼
                                          M0.6 gate (tiered, fail-closed)
                                                    │
                              ┌─────────────────────┼─────────────────────┐
                              ▼                     ▼                     ▼
                        M0.7 diff/report      M0.8 CLI            M0.10 ratify/onboard
                              │                     │                     │
                              └──────────┬──────────┘                     │
                                         ▼                                │
                              M0.9 pre-commit + CI  ◀──────────────────────┘
                                         │
                                         ▼
                              M0.11 fixtures + acceptance  (built alongside from M0.2, gates the whole phase)
```

---

## M0.0 — Project scaffold

**Intent.** A buildable, testable, CI-linted Go project with the layout from `01 §2`, licensed GPL-3.0.

**Tasks.**
- `go mod init`, choose CLI framework (cobra) and errgroup; set up `golangci-lint`, `go test`, and a `make check` target.
- Create the package skeleton (`internal/…`) with interface stubs so imports compile.
- Add `LICENSE` (GPL-3.0), `README` stub, `CONTRIBUTING` note that the plane contract is the extension surface.
- Set up Grip's *own* CI (build, vet, lint, test) — separate from the CI *integration* Grip ships (M0.9).

**Deliverables.** Compiling repo; `grip --version`; green CI on an empty test.
**Exit criteria.** `make check` passes; binary builds statically.

---

## M0.1 — Manifest & config: schema, loader, module discovery

**Intent.** Load intent (small, co-located) and repo config, and discover governed vs ungoverned modules deterministically.

**Tasks.**
- Implement `internal/manifest`: YAML parse + strict validation of the `grip.yaml` schema (`01 §5`). Unknown top-level keys preserved (forward-compat for later planes); unknown keys *inside* `architecture` rejected (fail-closed).
- Implement `internal/config`: `.grip.yaml` load + defaults + validation (`01 §6`), including plane enable flags and tier `promote` entries validated against declared rule ids (needs `Rules()` from M0.2/M0.5).
- **Module discovery:** walk `languages.*.roots`; every directory containing `grip.yaml` is a governed module; record file→module mapping. Directories without a manifest under a governed root are **ungoverned** (FR-14).
- Nested modules: define precedence (nearest-ancestor `grip.yaml` owns a file). Document and test the rule.

**Deliverables.** `grip modules` lists governed/ungoverned with counts.
**Exit criteria.** Malformed manifest/config → exit `3`/block with a precise message; discovery is deterministic and order-stable. (GR-ENG-2, FR-2, FR-14.)

---

## M0.2 — Common Graph IR + plane plugin contract

**Intent.** Freeze the language-neutral IR (`01 §4`) and the plane interface (`02`). This is the load-bearing seam.

**Tasks.**
- Implement `internal/ir`: types, canonical sorting, JSON (de)serialize, `irVersion`, content hashing (NFR-1), confidence records (NFR-9).
- Implement `internal/plane`: the `Plane` interface, `Violation`/`RuleSpec`/`Tier` types, and a `Registry`.
- Write the **engine-core-purity test**: greps engine packages for hard-coded plane ids; fails if any leak outside `registry`/`config` (enforces `02 §6`).
- Golden-file tests: a hand-written IR round-trips and hashes stably.

**Deliverables.** IR schema doc (generated from Go types); registry with the Architecture plane stub registered.
**Exit criteria.** IR hash identical across 100 runs and across machines (CI matrix); purity test green. (GR-ENG-6, NFR-1.)

---

## M0.3 — TypeScript/JS deriver (wrap dependency-cruiser + ts-morph)

**Intent.** Produce the IR for TS/JS by normalizing existing analyzers (D3).

**Tasks.**
- `internal/derive/typescript`: invoke `dependency-cruiser` with a Grip-generated config, capture its JSON, map file-level edges to **module-level** edges (collapse to the owning module via M0.1 mapping), attach evidence (`file:line:symbol`).
- Exported-surface extraction: ship a small bundled `ts-morph` helper script that lists, per module, symbols reachable from outside (index re-exports, public entrypoints). Normalize into `exports` + `reachableFromOutside`.
- Confidence: mark `dynamic import()`, `require(variable)`, and `any`-typed re-exports as `reduced` (NFR-9).
- Tool discovery + version pin check; missing tool → fail-closed message with install hint.

**Deliverables.** Given a fixture TS module, emits correct IR (golden).
**Exit criteria.** Edges, exports, and cycles match hand-verified expectations on the TS fixture; determinism holds. (FR-1, GR-ENG-1.)

---

## M0.4 — PHP deriver (wrap deptrac + PHPStan/php-parser)

**Intent.** Same IR from PHP, proving the IR is genuinely language-neutral (D2) before the engine hardens around one language.

**Tasks.**
- `internal/derive/php`: invoke `deptrac` (Grip-generated layer/collector config mapping directories→modules), capture JSON, normalize to module-level edges + evidence.
- Exported-surface extraction via a PHPStan-based or `nikic/php-parser` helper: public classes/functions/methods reachable across the module boundary; namespace-aware.
- Confidence: mark variable-variables, `call_user_func` with dynamic names, magic `__call`, and reflection as `reduced`.
- Tool discovery/version pin; fail-closed on missing.

**Deliverables.** Given a fixture PHP module, emits correct IR (golden) with the **same schema** as TS.
**Exit criteria.** A mixed PHP+TS fixture derives into one merged IR; no engine code branches on language outside `internal/derive`. (FR-1, D2, NFR-11.)

---

## M0.5 — Reconciler + Architecture rule set

**Intent.** The pure, deterministic heart: declared intent vs derived IR → located, one-sentence violations.

**Tasks.** Implement `internal/reconcile` (generic engine) and `internal/plane/architecture.Reconcile` (rules). Tier A rules (block):
- **arch.illegal-dependency** (FR-3): an IR edge `A→B` where `B ∉ A.dependencies.allow`. Message names both modules, the file:line, and remedy ("add to allow-list or remove the dependency").
- **arch.facade-widening** (FR-4): a symbol reachable from outside a module but absent from its `facade`.
- **arch.cycle** (FR-5): a strongly-connected component >1 in the module graph (Tarjan on the IR), unless `cycles: allow-internal` and the cycle is intra-module.
- **arch.direction-violation** (FR-5): an edge against declared `policy.layers.order`.
- **arch.internal-reach** (FR-8): an edge/evidence targeting a non-facade (internal) symbol of another module.
- **arch.stale-declaration** (FR-6): a `facade` entry or `dependencies.allow` entry with no corresponding derived symbol/edge — symmetric drift (ACP §5.3).

Each violation: `RuleID`, `Tier`, `Location`, one-sentence `Message` (NFR-5), `Confidence`. A rule whose evidence sits in a `reduced`/`none` scope emits a **"cannot verify — blocked"** variant (fail-closed).

**Deliverables.** Table-driven tests: one fixture per rule, asserting exact message + location.
**Exit criteria.** All Tier A rules fire correctly and *only* correctly on fixtures; reconcile is pure (no I/O; deterministic under shuffled inputs). (FR-3…FR-8, GR-ARC-1, NFR-5.)

---

## M0.6 — The Gate: tiered policy, orchestration, fail-closed

**Intent.** Convert everything into a binary pass/block, failing closed, honoring tier promotion.

**Tasks.**
- `internal/gate`: run the plane loop (`02 §3`) over enabled planes; aggregate violations; decision = block iff any Tier A (or promoted Tier B) or any fail-closed condition.
- **Fail-closed conditions** (principle 6, NFR-6): missing manifest on a governed module referenced by a rule; deriver/tool error; `reduced`/`none` confidence touching a rule; unknown enabled plane; malformed config.
- Tier promotion: apply `.grip.yaml policy.promote` (B→A) using validated rule ids.
- Local vs CI modes: incremental changed-module derive locally (`internal/vcs`), full authoritative derive in CI, with IR-hash assertion available.
- Exit codes per `01 §8`.

**Deliverables.** `grip gate --local` / `--ci`.
**Exit criteria.** Every fail-closed condition blocks with a distinct reason; a promoted advisory blocks; a disabled plane is absent from the decision. (GR-ENG-4, FR-9, GR-X-2, GR-X-3, NFR-6.)

---

## M0.7 — Diff / report (the shape delta)

**Intent.** Let the architect approve/reject a *structural* change in seconds without reading implementations (ACP §5.6).

**Tasks.**
- `internal/diff`: compute before/after IR delta (git HEAD vs working tree / base vs head in CI): edges added/removed, surface widened/narrowed, modules added/removed, new cycles.
- Distinguish **intentional** changes (manifest edit present in the diff) from violations — render "the architect widened this facade on purpose" (principle 5, FR-11 legibility).
- `internal/report`: three renderers (human/JSON/SARIF, `01 §9`). Human report groups by tier, leads with blocks, each a single actionable sentence.

**Deliverables.** `grip diff` (shape delta), and gate output embeds the delta.
**Exit criteria.** On a scripted structural change, the diff names exactly what moved; a manifest-only change renders as intentional and passes; SARIF validates and shows inline in a test PR. (FR-10, GR-ENG-5, GR-X-5.)

---

## M0.8 — CLI surface

**Intent.** CLI-first operation (GR-X-1): whole gate, single plane, diff, from the command line.

**Tasks.** Cobra commands:
- `grip gate [--local|--ci] [--plane architecture] [--json|--sarif]`
- `grip derive [--language …]` (debug: dump IR)
- `grip diff [--base <ref>]`
- `grip modules` (governed/ungoverned)
- `grip ratify …` (M0.10)
- `grip init` (scaffold `.grip.yaml` + draft manifests, M0.10)
- `grip version` (prints Grip + resolved analyzer versions for reproducibility)

**Deliverables.** Documented `--help` for each; stable flags.
**Exit criteria.** Each command runs the intended engine path; `--plane` runs a single plane; exit codes correct. (GR-X-1.)

---

## M0.9 — Control-point integrations (pre-commit + CI)

**Intent.** Two control points: fast local, authoritative CI (GR-X-2, D6).

**Tasks.**
- **Pre-commit:** a hook script / `pre-commit` framework entry running `grip gate --local` on changed modules; fast, advisory-but-real.
- **GitHub Action** (`ci/github-action`): composite/Docker action running `grip gate --ci`, uploading SARIF for inline annotations, failing the check on block.
- **GitLab CI** (`ci/gitlab`): template job, code-quality/SARIF report artifact, `allow_failure: false` on block.
- Document tool provisioning (Node+dependency-cruiser, PHP+deptrac) inside the CI image.

**Deliverables.** Reusable action + template; a demo repo where a bad PR is blocked.
**Exit criteria.** A PR/MR that introduces an illegal dependency is blocked with an inline annotation; a clean PR passes. (GR-X-2.)

---

## M0.10 — Onboarding: generate-then-ratify

**Intent.** Avoid the brownfield backlog (PRD §15, ACP §11.6): accept current reality as the declared start (GR-X-4).

**Tasks.**
- `internal/ratify`: derive current IR, **generate draft `grip.yaml`** per module (facade = current exported surface; dependencies.allow = current edges; intent = empty placeholder for the human).
- `grip init` writes drafts + a starter `.grip.yaml`; `grip ratify` accepts current derived state as baseline (used later by planes with baselines, e.g. behavior).
- Report generated drafts distinctly so the human reviews/edits intent before governance bites.

**Deliverables.** On the fixture repo, `grip init` yields a passing baseline in one run.
**Exit criteria.** A fresh repo goes from zero manifests to a green gate in one sitting, with drafts small enough to read (NFR-3). (GR-ENG-9, FR-15, GR-X-4.)

---

## M0.11 — Fixture repos + acceptance harness

**Intent.** Since there's no real dogfood target (D8), synthetic PHP+TS fixtures are the proof. Built incrementally from M0.2 onward; gates the whole phase.

**Tasks.**
- Build `testdata/fixtures/` with: a clean multi-module PHP+TS repo; a library of scripted "bad agent" diffs (illegal dep, facade widening, cycle, internal-reach, stale decl, direction violation); "good" diffs; an intentional-manifest-edit diff; a `reduced`-confidence case (dynamic dispatch).
- Golden-output acceptance tests wiring each scripted diff → expected gate decision + report.
- Determinism CI matrix (Linux/macOS) asserting identical IR hash.

**Deliverables.** `make acceptance` runs the full matrix.
**Exit criteria.** Every "bad" diff blocks with the correct single-sentence report; every "good" diff passes; intentional edit passes-as-intentional; determinism holds. **This is the M0 gate.** (Full detail in file 08.)

---

## M0 exit checklist (unlocks M1)

- [ ] PHP **and** TS derive into one IR; no engine branch on language outside `internal/derive`.
- [ ] All Tier A architecture rules fire correctly on fixtures, and only correctly.
- [ ] Gate fails closed on every defined ambiguity/error condition.
- [ ] Shape diff renders intentional vs accidental change correctly.
- [ ] CLI + pre-commit + GitHub + GitLab all block a bad change and pass a clean one.
- [ ] Generate-then-ratify brings a fresh repo to green in one sitting.
- [ ] Determinism (IR hash) holds across runs and machines.
- [ ] Plane contract proven by the Architecture plane with zero engine coupling — ready for a second plane.
