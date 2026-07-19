# 09 · Risks, Open Questions & Sequencing

Standing reference for the whole build. Combines the PRD/ACP risk registers with plan-level answers, and defines the milestone dependency graph and the exit gates that unlock each next stage.

---

## 1. Milestone sequencing & exit gates

Each stage stands alone (principle 7) and must be **load-bearing** before the next starts (PRD §13, ACP §10). "Load-bearing" = its exit checklist is green on the fixtures *and* it has caught at least one real would-be-bad change in use.

```
M0 Engine + Architecture ──▶ M1 Test-rigor ──▶ M2 Behavior ──▶ M3 Contract ──▶ M4 Advisories + Visual
   (MVP; most control)        (makes trust     (closes          (wire-level      (optional; STOP
                               real)            semantic gap)     compat)          before studio)
```

| Transition | Unlock condition |
|------------|------------------|
| → M1 | M0 exit checklist (file 03) green; plane contract proven with zero engine coupling |
| → M2 | Vacuous contracts + tamper detection block on PHP+TS; M1 added with no engine changes (proves contract on a non-graph model) |
| → M3 | Behavior deltas gate + ratify works; nondeterminism degrades to reduced-confidence not false pins |
| → M4 | Breaking API/schema/migration changes block and name the break |

**Order rationale:** M1 before M2/M3 because behavior and contract planes *lean on tests*; verifying test rigor first makes their trust honest (the PRD's explicit reason for this ordering). M4 last and optional because it carries the studio-scope-creep risk and delivers the least control-per-effort.

---

## 2. Risk register (with plan-level mitigations)

| Risk | Severity | Mitigation in this plan |
|------|----------|-------------------------|
| **Scope creep to a studio** (editable authoritative visual) | High | Visual is M4, read-only, consumes JSON only; hard review rule rejects any manifest-editing affordance (file 07). Non-negotiable per PRD §4.3. |
| **False pass** (gate says clean when it isn't) | High | Pure/deterministic reconciler + exhaustive both-directions fixtures + mutation-testing Grip's own rules (file 08 §4). Fail-closed default. |
| **Boundary rigidity vs emergent design** | Med-High | Intentional change = cheap manifest edit rendered as intentional (principle 5, M0.7). Watch false-block rate in real use (success metric). |
| **Authoring overhead** (manifests grow large) | High | Generate-then-ratify (M0.10) over author-from-scratch; NFR-3 smallness as a hard test; `intent:` is human/advisory, not gated bloat. |
| **Multi-language IR overfit to one stack** | High | PHP + TS built together in M0 (D2); engine-core-purity test forbids language branching outside `internal/derive`; M1 stresses a non-graph model. |
| **Analyzer output changes break determinism** | Med | Tool version pinning + resolved-version capture in report; IR-hash assertion in CI (NFR-1). |
| **Analysis blind spots** (dynamic dispatch, reflection, cross-language) | Med | Reduced-confidence zones (NFR-9) that fail closed when they touch a rule, never a false clean result. |
| **Brownfield backlog** on real adoption | Med | Ratify/baseline path (GR-X-4, M0.10) accepts current reality as the start. |
| **Correlated test failure** (wrong code + wrong passing test) | Med | M1 mutation verification + independence practices (PRD §10). |
| **Mutation testing too slow** (M1) | Med | Full in CI, incremental+cached locally; determinism preserved. |
| **LLM leaks into the gate** | High | Structural: Tier C cannot change exit code (file 02); LLM confined to M4 `Derive`, advisory only (principle 3). |
| **CI tool provisioning drift** (Node/PHP analyzers in CI image) | Low-Med | Pinned toolchain in shipped action/template; missing tool = fail-closed with remediation. |

---

## 3. Open questions (carried from PRD §16 / ACP §11.7, with current stance)

| Question | Current plan stance | Revisit at |
|----------|---------------------|-----------|
| Right granularity of a "module" per stack; monorepo vs multi-repo | **Directory-based** default (D4); config hook reserved for package/namespace granularity (D9); monorepo-first, multi-repo cross-contracts deferred | M3 (contracts) forces the multi-repo question |
| How cross-module/cross-service contracts are owned without pulling internals in | Deferred; likely published contract artifact + consumer-side check | M3 design |
| Managing false-positive cost when Tier B is promoted to hard gate | Promotion is per-repo opt-in (M0.6 wiring); measure false-block rate before recommending any promotion | M4 (advisories) |
| Keeping the manifest authoritative for *intent* when intent is only partially machine-checkable | `intent:` stays human/advisory; machine-checkable parts (facade, deps) are gated; Tier C aids human notice | M4 |
| Capturing behavior snapshots cheaply and stably | Piggyback on existing tests; normalize; nondeterministic boundaries → reduced confidence not false pins | M2 design |

---

## 4. Success signals to watch in real use (PRD §4.2, §14)

Track once M0 is dogfooded on a real repo:
- Un-ratified structural changes reaching main → target **zero**.
- Share of changes reviewed via shape-diff vs line-diff → majority, trending up.
- Median manifest size → readable in under a minute (NFR-3).
- Gate false-block rate (Tier A) → low enough not to be routinely bypassed.
- Gate bypass rate → near zero.

**Failing signs** (halt and rethink): manifests growing large, team routinely bypassing the gate, or a visual layer becoming where people edit the design.

---

## 5. Immediate next actions after plan approval

1. **M0.0** scaffold the Go repo (layout in `01 §2`), GPL-3.0, CI.
2. Stand up the **fixture base repo** (file 08 §2.1) in parallel — it's needed to test everything else.
3. Lock the **IR schema** (`01 §4`) and **plane interface** (file 02) — the two hardest-to-change artifacts — before writing derivers.
4. Build TS + PHP derivers to the frozen IR; then reconciler → gate → diff → CLI → integrations, per the M0.x order (file 03).
