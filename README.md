# Grip

**A deterministic control plane that keeps a human the architect while AI agents implement.**

Grip lets you declare small **intent** at your module boundaries, **derives** the real structure from code, and **gates** any change that moves the system's shape without your consent. An agent physically cannot introduce an illegal dependency, widen a module's facade, create a dependency cycle, reach into another module's internals, or violate layer direction — in **PHP or TypeScript/JS** — without a red build. You review the *shape delta*, not the lines.

Grip is a single binary. Its first-party TypeScript and PHP helpers are embedded
and extracted to a content-addressed user cache at runtime, so consumer
repositories never copy `ci/helpers`. The helper source remains visible in this
repository for auditability.

## Why

Truth is **derived from code, never authored beside it**. You author only small things, only at the boundary (a `grip.yaml` you can read in under a minute). Enforcement is **deterministic — no LLM anywhere in the gate path**. The gate **fails closed**: ambiguity, a missing declaration, a missing tool, or an analysis error blocks. A false pass is the worst possible bug, so Grip prefers to block and explain.

## How it works

```
Declare (grip.yaml)  →  Derive (from code)  →  Reconcile (intent vs actual)  →  Gate (tiered, fail-closed)  →  Diff / Report (shape, not lines)
```

The engine owns Reconcile, Gate, and Diff/Report **generically**. Each *plane* supplies a manifest schema, a deriver, and a tiered rule set. Adding a language means adding a deriver; adding an axis of governance means adding a plane — **never** editing the engine. That seam is enforced by a test ([`internal/enginepurity`](internal/enginepurity)) that fails if the engine ever names a plane.

Derivers **wrap existing analyzers** (dependency-cruiser + ts-morph for TS; deptrac + php-parser for PHP) and normalize their output into one language-neutral **Common Graph IR**. Grip owns the IR, the reconciler, and all graph reasoning (cycles via its own Tarjan, reachability, direction). The IR is canonically sorted and content-hashed: the same commit + tool versions hash byte-identically across machines (NFR-1).

## Install / build

```sh
# release archive: unpack and put grip on PATH
# source checkout:
make build        # -> bin/grip
make check        # build + vet + gofmt + lint + test
make acceptance   # deterministic fixture matrix
```

Requires Go 1.26+ only to build from source. A real TypeScript/JavaScript gate
requires Node, `dependency-cruiser`, and `ts-morph`; PHP requires PHP, Deptrac,
and `nikic/php-parser`. Grip validates the actual analyzer identity and version
before it constructs an IR. Missing or incompatible tooling exits `2`.

For a complete field reference, examples, baseline workflow, and supported
surface, see [configuration](docs/configuration.md) and
[limitations](docs/limitations.md). The design and original requirements are
versioned under [plan](plan/) and [reqs](reqs/).

## Configure

Repo root — `.grip.yaml`:

```yaml
version: 1
planes:
  architecture: { enabled: true }
languages:
  typescript: { roots: ["src"], tool: { name: dependency-cruiser, minVersion: "16.0.0" } }
  php:         { roots: ["app"], tool: { name: deptrac, minVersion: "2.0.0" } }
policy:
  layers: { order: [domain, application, infrastructure] }
gate:
  local: { planes: [architecture] }
  ci:    { planes: [architecture] }
```

Per module — a directory with a `grip.yaml` becomes a governed module:

```yaml
module: billing
intent: >
  Owns invoice creation and money math. Must NOT send notifications or know HTTP.
architecture:
  facade: [createInvoice, Invoice]     # the deliberately public surface
  dependencies:
    allow: [src/money]                 # allowed outbound deps; absence = prohibition
    layer: domain
  cycles: forbid
```

## Use

```sh
grip gate --ci                # authoritative full gate (exit 0 pass / 1 block / 2 fail-closed / 3 usage)
grip gate --local             # fast local preview (pre-commit)
grip gate --plane architecture --sarif > grip.sarif
grip modules                  # governed vs ungoverned
grip derive                   # dump the Common Graph IR (debug)
grip diff                     # shape delta vs the ratified baseline
grip init                     # dry-run a .grip.yaml plus draft manifests (works without existing config)
grip init --write             # write only absent draft files
grip ratify                   # accept current derived state as the baseline
grip version
```

**Exit codes:** `0` pass · `1` hard violation · `2` fail-closed (missing tool/manifest, reduced confidence) · `3` config/usage error.

## The Architecture plane's rules (Tier A, blocking)

| Rule | Fires when |
|------|-----------|
| `arch.illegal-dependency` | a module depends on another not in its `dependencies.allow` |
| `arch.facade-widening` | a symbol is used from outside a module but absent from its `facade` |
| `arch.cycle` | modules form a dependency cycle |
| `arch.direction-violation` | a dependency points outward against `policy.layers.order` |
| `arch.internal-reach` | a caller reaches a non-facade internal of another module |
| `arch.stale-declaration` | a `facade`/`allow` entry has no backing derived reality |

A rule whose evidence lands in a reduced/none-confidence scope (dynamic `import()`, `call_user_func($x)`, reflection) emits a **"cannot verify — blocked"** result instead of a false pass.

## CI integrations and releases

- **GitHub Action:** [`ci/github-action`](ci/github-action) — installs a pinned
  Grip release, provisions pinned analyzers, runs the gate, and uploads SARIF.
- **GitLab template:** [`ci/gitlab/.gitlab-ci.yml`](ci/gitlab/.gitlab-ci.yml).
- **Pre-commit:** [`ci/hooks/pre-commit`](ci/hooks/pre-commit) and [`.pre-commit-hooks.yaml`](.pre-commit-hooks.yaml).

Release archives are produced by [GoReleaser](.goreleaser.yaml) for macOS and
Linux (amd64/arm64), with a version stamped into the binary and a checksum file.
Do not pin production automation to `main`.

## License

GPL-3.0. See [`LICENSE`](LICENSE).
