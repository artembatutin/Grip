# 07 · Phase M4 — Advisories + Read-Only Visualization (sketch)

**Last, and optional (PRD §13, ACP §10).** Do not start until M0–M3 are load-bearing. The dominant risk here is scope creep into a studio (PRD §15, ACP §11.1) — treat it as the hardest scope line in the product.

---

## Part A — Advisory passes (Tier B deterministic + Tier C judgment)

**Tier B — deterministic advisories (reported, don't block by default; promotable per repo).** These were *declared* in the Architecture plane's `Rules()` in M0 but implemented here:
- Cross-module duplication; co-change coupling hotspots (from `git` history); excessive delegation / pass-through (middle man); message chains; single-implementor abstractions / unused extension points (speculative generality); complexity threshold breaches.
- Derived from existing analyzers (jscpd-style clone detection, complexity tools) normalized into advisory signals. Any may be promoted to Tier A via `.grip.yaml policy.promote` (already wired in M0.6).

**Tier C — judgment-assisted (LLM or human; advisory only, NEVER blocks — principle 3).**
- Unclear/mysterious names, suspected data clumps, primitive obsession, ambiguous feature envy.
- This is the **only** place an LLM enters Grip. It runs inside a plane's `Derive`, emits **only Tier C** violations, and the engine structurally prevents Tier C from changing the exit code (file 02 §2). Clearly marked non-blocking in every report (GR-X-6, FR-16).

## Part B — Read-only visualization (GR-X-7, FR-17, NFR-10)

- Renders the derived graph with manifest overlays (allowed vs actual, violations highlighted) and the per-change shape diff visually. Consumes the existing `--json` report — no new source of truth.
- **Strictly read-only and strictly derived.** It can never be edited to change intended architecture. An editable authoritative diagram reintroduces the exact drift Grip exists to kill (non-negotiable: PRD §4.3, ACP §2.2/§11.1).
- Ships as a static viewer over the JSON output; no server authority.

## Guardrails for this phase
- A hard review rule: **any** feature that lets a user change a manifest, edge, or facade *through the visual* is rejected on sight.
- Stop after the read-only visual. There is no M5 studio.

## Exit criteria
Advisories surface on fixtures without blocking (unless promoted); LLM pass is provably unable to affect the gate decision; the visual renders the derived graph + diff and offers no editing affordance whatsoever.
