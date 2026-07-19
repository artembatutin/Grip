# 01 · Architecture & Technical Decisions

Deep technical design for the Go engine and the artifacts it reads/writes. Everything here is M0-grade; later planes extend but do not contradict it.

---

## 1. System shape

Grip is a **single Go binary** that orchestrates external analyzers and applies deterministic policy. Nothing in the hot path depends on a network service or an LLM.

```
                    ┌─────────────────────────────────────────────┐
                    │                 grip (Go binary)            │
                    │                                             │
  repo source ─────▶│  Config loader  ─▶  Plane registry         │
  .grip.yaml        │        │                    │              │
  **/grip.yaml ────▶│  Manifest loader           ▼              │
                    │        │           Deriver orchestrator ───┼──▶ subprocess: dependency-cruiser (TS)
                    │        │                    │              │──▶ subprocess: deptrac / PHPStan (PHP)
                    │        ▼                    ▼              │
                    │   Intent model      Common Graph IR        │
                    │        └────────┬───────────┘              │
                    │                 ▼                          │
                    │            Reconciler ──▶ Violation set     │
                    │                 │                          │
                    │                 ▼                          │
                    │      Gate (tiered policy, fail-closed)      │
                    │                 │                          │
                    │        ┌────────┴────────┐                 │
                    │        ▼                 ▼                 │
                    │   Diff / Report     Exit code (0/nonzero)  │
                    └─────────────────────────────────────────────┘
                             │
                    outputs: human report (stdout), JSON (--json), SARIF (--sarif for CI)
```

**Key property:** derivers are the *only* language-aware components. The IR, reconciler, gate, and reporter are language-agnostic. Adding C# later (D9) means adding a deriver, not touching the engine.

---

## 2. Grip's own repository layout

Go module, standard layout, so contributors (GPL-3.0, open source — D6) find things where expected.

```
grip/
├── cmd/grip/                 # main() — CLI entrypoint, wires cobra commands
├── internal/
│   ├── config/               # .grip.yaml load + validate + defaults
│   ├── manifest/             # grip.yaml load, schema, module discovery
│   ├── ir/                   # Common Graph IR types + JSON (de)serialization + schema version
│   ├── plane/                # Plane plugin contract (interfaces) + registry
│   │   └── architecture/     # M0 plane: manifest section, rules, reconcile logic
│   ├── derive/
│   │   ├── orchestrator.go   # runs derivers concurrently, merges IR, tracks confidence
│   │   ├── driver.go         # Deriver interface + subprocess runner + cache
│   │   ├── typescript/       # wraps dependency-cruiser (+ ts-morph for exports)
│   │   └── php/              # wraps deptrac (+ PHPStan for exports/symbols)
│   ├── reconcile/            # generic declared-vs-derived engine
│   ├── gate/                 # tiered policy, fail-closed orchestration, exit codes
│   ├── diff/                 # before/after IR delta → shape diff
│   ├── report/               # human text, JSON, SARIF renderers
│   ├── ratify/               # generate-then-ratify (draft manifests, baselines)
│   └── vcs/                  # git plumbing (changed files, HEAD vs working tree, history)
├── planes/                   # (later) out-of-tree plane examples / docs
├── ci/
│   ├── github-action/        # composite action + Dockerfile
│   └── gitlab/               # .gitlab-ci template
├── testdata/
│   └── fixtures/             # synthetic PHP+TS repos (see file 08)
├── docs/
├── LICENSE                   # GPL-3.0
└── go.mod
```

`internal/` is used deliberately: the plane contract is the only intended extension surface, and it is promoted to a stable package when M1 starts (D9 forces this discipline early).

---

## 3. External tool dependencies (the "assemble, don't reimplement" list)

Per D3 and PRD NFR-8. Grip declares these as **optional external binaries** discovered at runtime; a missing tool for an enabled language is a **fail-closed** condition with a clear remediation message, never a silent skip.

| Concern | TypeScript/JS | PHP | Notes |
|---------|---------------|-----|-------|
| Dependency graph | `dependency-cruiser` | `deptrac` | Both emit machine-readable graphs; Grip normalizes to IR |
| Exported surface | `ts-morph` script (Grip ships a tiny bundled Node helper) | PHPStan / `nikic/php-parser` helper | Extract what is *actually* reachable across a module boundary |
| Cycles / direction | derived from graph in Go | derived from graph in Go | Cycle detection is Grip's own (Tarjan) on the IR — deterministic, tool-independent |
| Version-control history | `git` | `git` | Co-change coupling (Tier B, later) |

**Design rule:** Grip never parses PHP or TS itself in M0. It consumes JSON from the analyzers and computes graph properties (cycles, reachability, direction) on the normalized IR. This keeps the M0 custom-code surface small (the PRD's explicit goal) and pushes language complexity into tools that already solved it.

**Version pinning for determinism (NFR-1):** the resolved analyzer versions are captured in the run report and, optionally, asserted against `.grip.yaml`. A tool version change that alters output is surfaced, not silently absorbed.

---

## 4. The Common Graph IR

The contract between derivers and the engine. One schema, language-neutral, versioned. This is the single most important internal artifact — get it right and every future stack (D9) plugs in cleanly.

### 4.1 Conceptual model

- **Node** = a governed unit. In M0, a *module* (directory with `grip.yaml`) and its *symbols* (exported/reachable names). Files map to modules; the IR records the mapping so violations can point at a file:line.
- **Edge** = a directed dependency `from → to` at module granularity, carrying the underlying evidence (which file, which symbol, import vs call).
- **Surface** = per module, the set of symbols actually reachable from outside it.
- **Confidence** = per node/edge, a signal of analysis reliability (see §4.3).

### 4.2 Shape (illustrative JSON)

```json
{
  "irVersion": "1",
  "commit": "…",                      // resolved by engine, not deriver
  "language": "typescript",
  "modules": [
    {
      "id": "src/billing",             // repo-relative dir = module id
      "files": ["src/billing/index.ts", "src/billing/invoice.ts"],
      "exports": [
        { "name": "createInvoice", "kind": "function", "file": "src/billing/index.ts", "line": 12 }
      ],
      "reachableFromOutside": ["createInvoice"]   // actual surface
    }
  ],
  "edges": [
    {
      "from": "src/billing",
      "to": "src/notifications",
      "kind": "import",
      "evidence": [{ "file": "src/billing/invoice.ts", "line": 3, "symbol": "sendEmail" }]
    }
  ],
  "confidence": [
    { "scope": "src/billing/legacy.ts", "level": "reduced", "reason": "dynamic import() not statically resolvable" }
  ]
}
```

### 4.3 Confidence model (implements NFR-9, honest confidence)

Every deriver must classify what it could and could not see. Levels: `full`, `reduced`, `none`. A `reduced`/`none` scope that is *relevant to a rule* changes the gate outcome to **block with an explicit "cannot verify" reason** rather than a false pass (fail-closed, principle 6). This is how Grip avoids "clean result it cannot back up" (ACP §11.5). Dynamic dispatch, reflection, PHP variable-variables, and cross-language calls are the expected `reduced` sources.

### 4.4 Determinism requirements (NFR-1)

- Modules, edges, exports, and violations are **sorted canonically** before hashing/output.
- No timestamps or absolute paths in the IR (repo-relative only).
- The IR for a given commit + tool versions hashes identically across machines. The engine records this hash; CI can assert it.

---

## 5. Manifest schema (`grip.yaml`, per module) — D4

Small enough to read in under a minute (NFR-3). Each plane contributes a top-level section; M0 defines only `architecture`. Unknown sections are preserved untouched (forward-compat for later planes).

```yaml
# src/billing/grip.yaml
module: billing                 # human name; id is the directory path
intent: >                       # single responsibility + anti-responsibilities
  Owns invoice creation and money math. Must NOT send notifications,
  talk to the network, or know about HTTP.

architecture:
  facade:                       # the deliberately exposed surface. Everything else is internal.
    - createInvoice
    - Invoice
  dependencies:                 # allowed OUTBOUND deps. Absence = prohibition.
    allow:
      - src/money
      - src/persistence         # (optionally) direction/layer can be constrained
    layer: domain               # optional: participates in layered-direction rules
  cycles: forbid                # forbid | allow-internal (default forbid)
```

**Rules the loader enforces:**
- A `grip.yaml` makes its directory a **governed module**. No file ⇒ **ungoverned**, reported distinctly (FR-14), never silently ignored.
- `facade` entries must resolve to real exported symbols at derive time; a facade entry with no matching export is a **stale declaration** (FR-6).
- `dependencies.allow` lists *module ids* (directory paths) or declared layer names.
- The manifest is intentionally tiny; anything requiring paragraphs belongs in `intent:` (advisory/human-owned, not gated).

---

## 6. Repo config schema (`.grip.yaml`, repo root) — D5

```yaml
version: 1
planes:
  architecture: { enabled: true }
  testRigor:    { enabled: false }     # M1
  behavior:     { enabled: false }     # M2
  contract:     { enabled: false }     # M3

languages:
  typescript:
    roots: ["src", "packages"]
    tool: { name: dependency-cruiser, minVersion: "16.0.0" }
  php:
    roots: ["app", "src"]
    tool: { name: deptrac, minVersion: "2.0.0" }

modules:
  granularity: directory              # D4; future: package|configurable (D9)

policy:
  promote:                            # Tier B → Tier A promotions (per repo, PRD §9 / ACP §7)
    - rule: duplication
      to: block
  layers:                             # optional layered-architecture direction rules
    order: [domain, application, infrastructure]

gate:
  failClosed: true                    # cannot be disabled; present for auditability only
  local:  { planes: [architecture] }  # fast pre-commit subset
  ci:     { planes: [architecture] }  # authoritative
```

Fail-closed behaviors from config: unknown plane name → error; enabled language with missing tool → block; malformed config → block. Silence is never approval (principle 6).

---

## 7. Concurrency & performance model (NFR-4)

- Derivers for different languages/roots run **concurrently** (goroutines + `errgroup`), each in its own subprocess. Wall-clock ≈ slowest single deriver, not the sum.
- **Incremental mode (local/pre-commit):** derive only modules touching the changed file set (from `internal/vcs`), reusing a cached IR for untouched modules keyed by content hash. Cache is an optimization only — a cold run must produce byte-identical results (determinism trumps cache).
- **Authoritative mode (CI):** full derive, no cache trust, IR hash asserted. This is the source of truth; local is a fast preview.
- Target: local gate on a mid-size module set completes in the low seconds; CI may take longer and be more thorough.

---

## 8. Control points & exit codes (D6)

| Control point | Command | Speed | Authority |
|---------------|---------|-------|-----------|
| Local pre-commit | `grip gate --local` (via hook) | fast, incremental | advisory preview |
| CI pre-merge | `grip gate --ci` | thorough, full | **authoritative** |

Exit codes (stable, scriptable): `0` pass · `1` blocked (hard violation) · `2` fail-closed (analysis error / missing tool / missing manifest) · `3` config/usage error. CI distinguishes `1` (fix code or ratify) from `2` (environment/analysis problem).

---

## 9. Reporting outputs

Three renderers over one violation/diff model (report package):
- **Human (default):** each violation as one plain sentence — rule, location (`file:line`), remedy (NFR-5). Shape diff as an edges-added/removed, surface-widened/narrowed summary.
- **`--json`:** machine-readable, stable schema, for tooling and the future visual (M4).
- **`--sarif`:** for GitHub/GitLab code-scanning UIs, so blocks show inline on PRs/MRs.

Intentional changes (manifest edits) are rendered as such — "the architect widened this facade on purpose" — not as mystery violations (ACP §5.6, principle 5).

---

## 10. What is deliberately NOT built in M0

To keep the custom surface small (PRD §13) and honor the non-goals:
- No LLM anywhere in the gate path (Tier C is M4, advisory only).
- No visual/graph UI (M4, read-only).
- No native language parsing (wrap tools — D3).
- No mutation/behavior/contract logic (M1–M3).
- No auto-fix (out of scope, PRD §17).

The plane plugin contract (file 02) is built in M0 even though only one plane exists, because proving it with a second plane (M1) is cheap only if the seam was designed in from the start.
