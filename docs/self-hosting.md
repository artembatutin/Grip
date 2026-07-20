# Self-hosting Grip

Grip governs its own production Go packages with all four planes. The root
[`.grip.yaml`](../.grip.yaml) selects the `ci`, `cmd`, and `internal` source
trees; each production package owns a small colocated `grip.yaml` declaring its
responsibility, facade, allowed dependencies, and architectural layer.

Architecture governs all 22 production modules. `internal/toolversion` is the
first fully governed black box across the other three planes:

| Plane | Real evidence | Adversarial proof |
|---|---|---|
| Architecture | `go list` plus Go AST dependency/surface evidence | A compile-valid undeclared import is blocked |
| Behavior | A Go example executed and verified by `go test` | Coordinated code+expected-output drift is blocked until re-pinned |
| Contract | Canonical exported Go declaration signatures | A compile-valid signature break is blocked until re-ratified |
| Test rigor | Marked boundary test plus real Go-overlay mutants | Vacuous/skipped/deleted tests and threshold tampering are blocked |

Run the local proof with:

```sh
make dogfood
make proof
```

`make dogfood` builds the candidate binary and runs the unified gate over the
working tree. `make proof` first executes the adversarial matrix for every plane,
then runs the self-gate, confirms no shape delta against the ratified baseline,
and requires two complete JSON gate reports to be byte-identical.

## Release guardian

A candidate binary must not remain its own sole judge: a defect in a changed
deriver or gate could approve itself. After the first release containing Go
support, CI must run both checks:

1. Install a pinned, previously released Grip version and gate the PR source.
2. Build the PR candidate and gate the same source again.

The pinned release is the guardian for ordinary changes. Upgrading the guardian
is an explicit follow-up after the new release is published. Changes to
`.grip.yaml`, module `grip.yaml` files, and `.grip/` baselines are design
decisions; `CODEOWNERS` marks them for mandatory human review when branch
protection is enabled.
