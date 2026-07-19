# 06 · Phase M3 — Contract Plane (sketch)

**Governs.** The boundaries at the wire — service APIs, event/message schemas, and database schema (PRD §7.4).

**Human declares.** The intended external contract, or adopts the current one as baseline (reuses generate-then-ratify, M0.10).

**Grip derives.** The actual contract shape from code and schema definitions, compared against declared and previous versions.

**Blocks (Tier A).** Backward-incompatible changes — removed/renamed fields in use, incompatible migrations, event-shape breaks — against declared compatibility rules (GR-CON-1).
**Warns (Tier B).** Additive changes and deprecations pending consumer sign-off (GR-CON-2).
**Value.** An agent cannot ship a consumer-breaking change without a red build and a "this breaks X" report.

---

## Fit to the plane contract

Same `Plane` interface. Stresses **versioned/temporal comparison** (current vs previous vs declared) and heterogeneous sub-derivers per contract kind.

- **Manifest section:** `contract: { api: {…}, events: {…}, db: { compat: backward } }`, with compatibility policy (backward/forward/full).
- **Deriver (wrap existing checkers, NFR-8):**
  - **API:** derive from code/OpenAPI; diff with an OpenAPI-diff / breaking-change checker.
  - **Events/messages:** JSON Schema / Protobuf / Avro compatibility checkers.
  - **DB schema/migrations:** parse migration files; a migration-compat checker for destructive/incompatible changes.
  - PHP + TS specifics: derive API surface from the respective frameworks' route/schema definitions.
- **Reconcile (pure):** compare derived-current against declared + previous per the module's `compat` policy → `contract.breaking-*` (Tier A) or `contract.additive-*` (Tier B). The "this breaks X" report names the removed/renamed element and, where known, the consumer.

## Open questions carried (PRD §16)
- **Cross-service / cross-repo contracts:** how a producer and consumer in separate repos (D8 multi-repo future) share a contract without pulling internals into scope. Likely a published contract artifact + a consumer-side check; deferred design.

## Exit criteria (unlocks M4)
On fixtures: a removed/renamed in-use API field blocks; an incompatible migration blocks; an additive field warns; the report names what broke; folds into the same single gate and shape diff as all prior planes.
