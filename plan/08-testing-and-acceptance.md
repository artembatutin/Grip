# 08 · Testing & Acceptance Strategy

Grip is a gate: its own correctness is safety-critical, and a false pass is the worst failure mode (it silently returns the drift Grip exists to prevent). Testing is therefore a first-class deliverable, not a follow-up. Because there's no real dogfood repo (D8), **synthetic fixture repos are the primary proof** and are built alongside M0.

---

## 1. Test layers

| Layer | What it covers | Where |
|-------|----------------|-------|
| Unit (pure) | `Reconcile` rule logic, IR sorting/hashing, diff computation — deterministic, no I/O | table-driven Go tests |
| Deriver integration | Each language deriver produces correct IR from real analyzer output | golden IR files per fixture module |
| Engine integration | config + manifest + gate loop + fail-closed conditions | in-process, mocked derivers |
| **Acceptance (end-to-end)** | scripted "agent" diffs → real gate → expected decision + report | `make acceptance` over fixtures |
| Determinism | identical IR/decision hash across runs + OS matrix | CI matrix (Linux/macOS) |
| Contract/CI | GitHub Action + GitLab template block a bad PR/MR, pass a clean one | demo repo in CI |

The **pure/deterministic split** matters: everything that decides pass/block (`Reconcile`, gate policy) is pure and exhaustively unit-tested; everything that touches tools (`Derive`) is isolated behind `DeriveServices` and tested against recorded tool output so the suite is fast and hermetic.

---

## 2. Fixture repos (`testdata/fixtures/`)

A synthetic **PHP + TS multi-module** repo mirroring a realistic layered app, plus a library of diffs. Built incrementally from M0.2; it *is* the M0 exit gate (M0.11).

### 2.1 Base repo (clean, passes the gate)
- TS side: e.g. `src/domain`, `src/application`, `src/infrastructure`, each with `grip.yaml` (facade, allowed deps, layer).
- PHP side: parallel `app/Domain`, `app/Application`, `app/Infrastructure` with PSR-4 namespaces.
- A declared layer order (`domain → application → infrastructure`) to exercise direction rules.
- One deliberately **ungoverned** module (no `grip.yaml`) to exercise FR-14.
- One **reduced-confidence** spot (a dynamic `import()` / PHP `call_user_func($name)`) to exercise NFR-9 fail-closed.

### 2.2 Scripted "bad agent" diffs — each MUST block
| Fixture diff | Expected Tier A rule | Asserted in report |
|--------------|----------------------|--------------------|
| `infrastructure` imported by `domain` | `arch.illegal-dependency` | both modules + file:line + remedy |
| new exported symbol not in facade | `arch.facade-widening` | symbol name + module |
| A→B→A introduced | `arch.cycle` | the cycle members |
| edge against layer order | `arch.direction-violation` | offending edge + layers |
| caller reaches a non-facade internal | `arch.internal-reach` | target internal symbol |
| facade lists a now-deleted export | `arch.stale-declaration` | stale entry |
| rule evidence lands in dynamic-dispatch zone | fail-closed "cannot verify" | reduced-confidence reason |
| enabled language, analyzer tool absent | fail-closed exit `2` | install remediation |
| governed module manifest deleted | fail-closed | missing-manifest reason |

### 2.3 "Good" diffs — each MUST pass
- Internal refactor within a module (no boundary change).
- Adding an allowed dependency already in `dependencies.allow`.
- Adding a new export **and** declaring it in `facade` in the same diff.

### 2.4 Intentional-change diffs — MUST pass and render as intentional
- Manifest edit widening a facade on purpose → passes, shape diff says "architect widened this facade on purpose" (principle 5, FR-11).
- Manifest edit adding an allowed dependency → passes, rendered as intentional.

---

## 3. Requirement → test traceability (M0 subset)

Every Must requirement has at least one owning test. Maintained as a matrix so coverage gaps are visible.

| Requirement | Verified by |
|-------------|-------------|
| GR-ENG-1 / FR-1 (derive per change, deterministic) | deriver golden tests + determinism matrix |
| GR-ENG-2 / FR-2 (co-located versioned manifests) | manifest loader + discovery tests |
| GR-ENG-3 / FR-3…8 (reconcile → located violations) | reconciler table tests + acceptance §2.2 |
| GR-ENG-4 / FR-9 (single fail-closed gate) | gate integration + fail-closed fixtures |
| GR-ENG-5 / FR-10 (unified shape diff) | diff tests + acceptance §2.4 |
| GR-ENG-6 (plugin contract, no engine coupling) | engine-core-purity test (M0.2) |
| GR-ENG-9 / FR-15 (generate-then-ratify) | `grip init` brings fixture to green in one run |
| GR-ARC-1 (arch hard gates) | acceptance §2.2 |
| GR-X-1 (CLI) | per-command tests |
| GR-X-2 (control points) | pre-commit + GitHub + GitLab demo-repo CI |
| GR-X-4 (ratify/baseline) | ratify tests |
| GR-X-5 / NFR-5 (one-sentence located remedy) | assert exact message strings in acceptance |
| NFR-1 (determinism) | IR-hash matrix, 100× repeat |
| NFR-6 (fail-closed) | every fail-closed fixture |
| NFR-9 (honest confidence) | reduced-confidence fixture |

---

## 4. Anti-false-pass discipline

- **Mutation-test Grip's own reconciler** (dogfood M1 early on Grip itself): if a mutant of a rule survives the fixture suite, the fixtures are too weak.
- **Both-directions coverage:** every rule needs a positive (fires) *and* a negative (doesn't fire on the legitimate near-miss) fixture, to bound false-block rate (a success metric, PRD §4.2).
- **Golden-report review:** report strings are golden files; changing a user-facing message is a visible, reviewed diff.

---

## 5. Performance acceptance (NFR-4)
- Local incremental gate on the fixture set completes in the low seconds (assert an upper bound in CI, generously, to catch regressions not to microbenchmark).
- CI full gate has no strict time bound but must not regress > agreed factor run-over-run.
