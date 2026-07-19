# 04 · Phase M1 — Test-Rigor Plane (sketch)

**Why second (not last).** It makes every other plane's trust *real*: "covered with tests" becomes a claim you can bank, which is what makes the black-box model honest (PRD §7.5, §13). Behavior and contract planes lean on tests; verify the tests first.

**Governs.** Whether the tests the other planes rely on actually mean anything.

**Human declares.** Per module, what "really covered" means — which behaviors must be meaningfully tested (small addition to `grip.yaml`, `testRigor:` section).

**Grip derives.** Test effectiveness via **mutation testing** (do tests fail when code is broken?), plus detection of skipped/deleted tests, over-mocking of the unit under test, and coverage-threshold tampering.

**Blocks (Tier A).** A boundary contract test that survives mutation (vacuous); a silently deleted or skipped *required* test; a lowered threshold on a governed module.
**Warns (Tier B).** Declining mutation score; rising mock ratio.
**Reports (FR-12/GR-TST-3).** Modules that hide internals but carry no verified boundary contract → **unverified**.

---

## Fit to the plane contract

Implements the same `Plane` interface (file 02). Proves the seam holds for a **non-graph** derived model (mutation scores, test inventory) — the key stress test that the IR/engine didn't overfit to Architecture.

- **Manifest section:** `testRigor: { requiredBehaviors: […], mutationThreshold: n, boundaryContract: true }`.
- **Deriver (wrap existing tools, per NFR-8):**
  - TS/JS: **Stryker Mutator**; test inventory + skip/`.only`/`.skip` detection from the test runner; coverage from the runner's report.
  - PHP: **Infection**; PHPUnit skip/`@group`/`markTestSkipped` detection; coverage from PHPUnit.
  - Threshold-tamper + deleted-test detection uses `internal/vcs` history (compare required-test set and thresholds against baseline).
- **Reconcile (pure):** mutation survivors on a boundary contract → `test.vacuous-contract`; missing required test vs baseline → `test.deleted-required-test`; skipped required test → `test.skipped-required-test`; lowered governed threshold → `test.threshold-tamper`.

## Trust-model tie-in (PRD §10, ACP §9)

- Mutation verification breaks **correlated failure** (wrong code + wrong passing test): a test that doesn't fail on a mutant is proven vacuous.
- **Independence practices:** support authoring/reviewing contracts in a pass separate from implementation; encourage property-based tests. These are process affordances Grip surfaces, not gates.
- **Unearned trust visible:** hidden internals + no verified boundary contract = reported unverified (GR-TST-3).

## Key risks specific to M1
- Mutation testing is **slow** → run full in CI, incremental/changed-only locally; cache mutant results by content hash. Determinism preserved (same commit → same survivors).
- Flaky tests corrupt mutation signal → detect and quarantine flaky tests before scoring.

## Exit criteria (unlocks M2)
Vacuous boundary contracts block on both PHP and TS fixtures; deleted/skipped/threshold tamper each block; unverified modules are reported; performance acceptable via incremental+cache; plane added with **zero engine changes** (contract validated on a non-graph model).
