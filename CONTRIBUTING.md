# Contributing to Grip

Grip is GPL-3.0, open source. Thanks for helping.

## The one rule that matters: the plane contract is the extension surface

Grip is **one engine and N policy planes, not N tools**. The engine
(`internal/ir`, `internal/reconcile`, `internal/gate`, `internal/diff`,
`internal/report`, `internal/derive`) is **plane-agnostic and must stay that
way**. It names no plane and branches on none.

- To add an **axis of governance** (test-rigor, behavior, contract), implement
  [`plane.Plane`](internal/plane/plane.go) in a new `internal/plane/<name>`
  package and register it in the single wiring point,
  [`cli.BuildRegistry`](internal/cli/cli.go). Do **not** touch the engine.
- To add a **language**, implement a `derive.Deriver` (see
  `internal/derive/typescript`) plus a bundled helper in `ci/helpers`. The IR and
  every rule are language-neutral; only derivers know a language.

The [`internal/enginepurity`](internal/enginepurity) test fails the build if any
plane id appears as a string literal in engine-core code. If it goes red, the
seam is wrong — fix the design, not the test.

## Hard invariants

1. **`Reconcile` is pure and deterministic.** No clock, no filesystem, no
   network, no map-iteration-order leaks. All I/O lives in `Derive` behind
   `DeriveServices` so it can be recorded in tests.
2. **The gate fails closed.** A false pass is the worst bug. When in doubt,
   block with a distinct, actionable reason and add a redundant test.
3. **Determinism (NFR-1).** Canonically sort before hashing/output. No
   timestamps or absolute paths in the IR. The IR hash is asserted in CI.
4. **One plain sentence per finding (NFR-5).** User-facing strings are golden
   files (`internal/plane/architecture/messages_test.go`); changing one is a
   deliberate, reviewed diff.

## Workflow

```sh
make check        # build + vet + gofmt + lint + test — run before every commit
make acceptance   # the end-to-end M0 gate over the fixtures
```

- Every rule needs a **positive** fixture (it fires) *and* a **negative**
  near-miss (it does not fire on the legitimate case), to bound the false-block
  rate.
- Derivers are tested against **recorded** analyzer output (golden), so the
  suite is fast and offline. Add a fixture scenario under
  `testdata/fixtures/scenarios/` and a row in the acceptance matrix.

## Testing the tools that aren't installed

The engine's correctness is proven offline: `internal/derive.RecordedRunner`
replays committed analyzer reports. The bundled `ci/helpers/*` scripts are the
real-tool adapters, exercised when Node/PHP + the analyzers are present.
