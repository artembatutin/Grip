# Common Graph IR — schema (v1)

The IR is the language-neutral contract between derivers and the engine
(`internal/ir`). It is versioned (`irVersion`), contains **no maps, no
timestamps, and no absolute paths**, and every slice is canonically sorted
before hashing or output, so a given commit + tool versions hashes
byte-identically across machines (NFR-1).

```jsonc
{
  "irVersion": "1",
  "commit": "…",                    // engine-supplied identity (not from a deriver)
  "modules": [
    {
      "id": "src/billing",          // repo-relative directory = module id (D4)
      "language": "typescript",     // per-module, so PHP+TS merge into one IR (D2)
      "files": ["src/billing/index.ts"],
      "exports": [                  // entrypoint (public) surface candidates
        { "name": "createInvoice", "kind": "function", "file": "src/billing/index.ts", "line": 12 }
      ],
      "reachableFromOutside": ["createInvoice"],  // exports actually used across the boundary
      "layer": "domain"             // optional; echoed from the manifest
    }
  ],
  "edges": [
    {
      "from": "src/billing",
      "to": "src/notifications",
      "kind": "import",             // import | call | extends | implements
      "evidence": [
        { "file": "src/billing/invoice.ts", "line": 3, "symbol": "sendEmail" }
      ]
    }
  ],
  "confidence": [                   // honest confidence (NFR-9)
    { "scope": "src/billing/legacy.ts", "level": "reduced", "reason": "dynamic import() not statically resolvable" }
  ],
  "analyzers": [                    // resolved tool versions, hashed for reproducibility
    { "name": "dependency-cruiser", "version": "16.3.0", "language": "typescript" }
  ]
}
```

## Confidence levels

`full` · `reduced` · `none`. A `reduced`/`none` scope **inside a governed
module** flips the gate to a `cannot verify — blocked` result rather than a false
pass. Confidence outside governed modules does not gate.

## Determinism

- `ir.Graph.Canonicalize()` sorts modules, files, exports, edges, evidence,
  confidence, and analyzers, and dedups where duplicates are meaningless.
- `ir.Graph.Hash()` is the SHA-256 of the canonical JSON. It is asserted 100× in
  `internal/ir` and across the fixture in `internal/acceptance`, and across OSes
  in CI.
- `ir.Merge` folds per-language graphs into one, rejecting module-id collisions.

## Adding a language

Emit this schema from a new `derive.Deriver` (+ a helper in `ci/helpers`). No
engine change: the IR, reconciler, gate, diff, and report never branch on
language.
