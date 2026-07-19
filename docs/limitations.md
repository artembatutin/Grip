# Supported surface and limitations

Grip’s architecture helpers analyse `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`,
`.cjs`, `.mts`, `.cts`, and `.php`. They require the named external analyzers to
run successfully. Helpers are embedded in Grip; external analyzers are not.

TypeScript/JavaScript uses dependency-cruiser for a complete dependency cruise
and ts-morph for symbol-level import/export evidence. Project configuration is
discovered from `tsconfig.json`; dependency-cruiser and TypeScript configuration
control normal Node and path-alias resolution. Static ES imports and static
CommonJS `require()` calls are supported. Dynamic imports and computed requires
are reduced-confidence evidence and therefore fail closed in governed code.

PHP runs Deptrac and nikic/php-parser. Dynamic callables, variable variables,
reflection, magic dispatch, runtime class construction, parse failures, and
unresolved autoloading must be treated as reduced-confidence evidence. They do
not silently pass a gate.

The later test-rigor, behavior, and contract models are present, but their
production adapters require explicit project-level integration. Until those
adapters are configured and their external tools can provide a valid report, the
correct outcome is fail-closed rather than an empty successful result. No Tier C
advisor output can affect an exit code or deterministic report hash.

Grip governs source-derived structure and explicitly declared artifacts. It
cannot prove runtime behavior hidden behind dynamically generated code, network
services, reflection, or an analyzer that cannot execute. A green result means
the configured deterministic evidence was available and reconciled; it is not a
general security audit or a substitute for review.
