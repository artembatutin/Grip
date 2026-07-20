# Configuration reference

`<repo>/.grip.yaml` selects planes, languages, and gate modes. A `grip.yaml`
inside a source directory declares one co-located module boundary.

```yaml
version: 1
planes:
  architecture: { enabled: true }
languages:
  go:
    roots: ["internal", "cmd"]
    tool: { name: go, minVersion: "1.26.0" }
  typescript:
    roots: ["src"]
    tool: { name: dependency-cruiser, minVersion: "16.0.0" }
  php:
    roots: ["app"]
    tool: { name: deptrac, minVersion: "2.0.0" }
modules: { granularity: directory }
policy:
  layers: { order: [domain, application, infrastructure] }
  promote: []
gate:
  local: { planes: [architecture] }
  ci: { planes: [architecture] }
```

The supported architecture analyzers are `dependency-cruiser` for TypeScript/
JavaScript, `deptrac` for PHP, and the Go toolchain plus standard parser for Go.
Go test files are excluded from production architecture edges. `minVersion` uses semantic-version precedence;
pre-releases sort below their final release. Analyzer identity or a version that
cannot be resolved is a fail-closed result.

```yaml
module: billing
intent: Owns invoice creation and money math.
architecture:
  facade: [createInvoice, Invoice]
  dependencies:
    allow: [src/money]
    layer: domain
  cycles: forbid
```

`grip init` prints missing draft files and does not modify the repository.
`grip init --write` writes only paths that do not exist. `grip ratify` writes the
canonical architecture baseline at `.grip/baseline.json`; behavior and contract
ratification write their plane-specific artifacts below the module’s `.grip/`
directory. Commit those artifacts with the intentional change.

Output flags are exclusive: `grip gate --json` and `grip gate --sarif` cannot be
combined. All machine-readable output is written to stdout; diagnostics go to
stderr. Exit codes are `0` pass, `1` verified hard violation, `2` cannot verify,
and `3` configuration or command usage.

Tier A is blocking. Tier B is deterministic and advisory unless its documented
rule is promoted with `policy.promote`. Tier C is judgment-assisted, is never
eligible for promotion, and is excluded from the IR hash and gate decision.

The pre-commit hook is a local preview and normally fails closed when the Grip
binary is missing. A developer may explicitly bypass that preview with
`GRIP_ALLOW_MISSING=1`; CI never uses that escape hatch and remains authoritative.
