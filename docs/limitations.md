# Supported surface and limitations

Grip’s architecture helpers analyse `.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`,
`.cjs`, `.mts`, `.cts`, and `.php`. They require the configured analyzer to run
successfully. Helpers are embedded in Grip; external analyzers are not.

Go uses `go list` for active package/build-context resolution and the standard
Go parser for exported package symbols and located selector evidence. `_test.go`
files are excluded from production architecture edges. Dot imports are reported
as reduced-confidence because their unqualified symbols cannot be attributed
reliably. Build-tagged files follow the active Go environment, so repositories
targeting several GOOS/GOARCH combinations should run the gate in each supported
CI environment.

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

Go has production adapters for all four planes. Verified Go examples provide
behavior captures; exported declarations provide ratified API contracts; and
marked boundary tests (`grip:test behavior=<name> contract`) are exercised
against deterministic source mutants through Go overlays. Required-test deletion
and mutation-threshold tampering compare against Git HEAD. The Go mutator
currently covers comparison, arithmetic, and boolean mutations and caps work at
24 mutants per module.

PHP and TypeScript behavior, contract, and test-rigor adapters still require
their documented project-level helpers. Event-schema and database contract kinds
still require external checkers when matching schema or SQL artifacts exist. A
missing required adapter fails closed. No Tier C advisor output can affect an
exit code or deterministic report hash.

Grip governs source-derived structure and explicitly declared artifacts. It
cannot prove runtime behavior hidden behind dynamically generated code, network
services, reflection, or an analyzer that cannot execute. A green result means
the configured deterministic evidence was available and reconciled; it is not a
general security audit or a substitute for review.
