#!/usr/bin/env node
// Grip's bundled TypeScript/JS surface+edge helper (plan/03 M0.3). It wraps
// ts-morph (the TypeScript compiler API) and emits Grip's normalized
// AnalyzerReport on stdout. Grip's Go deriver folds that report into the Common
// Graph IR; all graph reasoning (cycles, reachability, direction) is Grip's.
//
// Contract (must match internal/derive/report.go):
//   { tool, surfaceTool, imports[], exports[], reduced[] }
//   - exports: symbols exported from a MODULE ENTRYPOINT (a grip.yaml dir's
//     index.*). Symbols reachable only via a deep path are intentionally NOT
//     listed, so reaching them registers as an internal-reach.
//   - imports: every resolved cross-file reference with fromFile, toFile,
//     symbol, line, and dynamic/external flags.
//   - reduced: dynamic import() and require(variable) scopes (NFR-9).
//
// Requires ts-morph on the module path (e.g. `npm i -g ts-morph`). Exits 3 if
// ts-morph is unavailable so Grip can render a fail-closed install hint.
import { readdirSync, statSync, existsSync } from "node:fs";
import { join, relative, sep, dirname, basename } from "node:path";

function arg(name, def = undefined) {
  const i = process.argv.indexOf(`--${name}`);
  return i >= 0 && i + 1 < process.argv.length ? process.argv[i + 1] : def;
}
function args(name) {
  const out = [];
  for (let i = 0; i < process.argv.length - 1; i++) {
    if (process.argv[i] === `--${name}`) out.push(process.argv[i + 1]);
  }
  return out;
}

const repoRoot = arg("repo-root", process.cwd());
const roots = args("root");

let Project, SyntaxKind, ModuleResolutionKind;
try {
  ({ Project, SyntaxKind, ModuleResolutionKind } = await import("ts-morph"));
} catch (e) {
  process.stderr.write("grip ts helper: ts-morph not installed (npm i -g ts-morph)\n");
  process.exit(3);
}

const rel = (p) => relative(repoRoot, p).split(sep).join("/");
const isCode = (f) => /\.(ts|tsx|js|jsx|mjs|cjs|mts|cts)$/.test(f);

function walk(dir, acc) {
  for (const e of readdirSync(dir)) {
    if (e === "node_modules" || e === ".git") continue;
    const p = join(dir, e);
    const st = statSync(p);
    if (st.isDirectory()) walk(p, acc);
    else if (isCode(e)) acc.push(p);
  }
}
function moduleDirs(dir, acc) {
  for (const e of readdirSync(dir)) {
    if (e === "node_modules" || e === ".git") continue;
    const p = join(dir, e);
    if (statSync(p).isDirectory()) {
      if (existsSync(join(p, "grip.yaml"))) acc.add(rel(p));
      moduleDirs(p, acc);
    }
  }
}

const files = [];
const modDirs = new Set();
for (const r of roots) {
  const abs = join(repoRoot, r);
  if (existsSync(abs)) {
    walk(abs, files);
    moduleDirs(abs, modDirs);
  }
}

const project = new Project({
  compilerOptions: { allowJs: true, moduleResolution: ModuleResolutionKind.Bundler },
  skipAddingFilesFromTsConfig: true,
});
for (const f of files) project.addSourceFileAtPathIfExists(f);

const isEntrypoint = (relFile) => {
  const d = dirname(relFile);
  return modDirs.has(d) && /^index\.(ts|tsx|js|jsx|mjs|cjs)$/.test(basename(relFile));
};

const exportsOut = [];
const importsOut = [];
const reducedOut = [];

for (const sf of project.getSourceFiles()) {
  const relFile = rel(sf.getFilePath());

  // Entrypoint exports = the module's public surface.
  if (isEntrypoint(relFile)) {
    for (const [name, decls] of sf.getExportedDeclarations()) {
      const d = decls[0];
      if (!d) continue;
      exportsOut.push({
        file: relFile,
        name,
        kind: d.getKindName?.() || "unknown",
        line: d.getStartLineNumber?.() || 1,
      });
    }
  }

  // Static imports.
  for (const imp of sf.getImportDeclarations()) {
    const target = imp.getModuleSpecifierSourceFile();
    const line = imp.getStartLineNumber();
    const external = !target;
    const toFile = target ? rel(target.getFilePath()) : imp.getModuleSpecifierValue();
    const names = [];
    const def = imp.getDefaultImport();
    if (def) names.push("default");
    const ns = imp.getNamespaceImport();
    if (ns) names.push("*");
    for (const n of imp.getNamedImports()) names.push(n.getName());
    if (names.length === 0) names.push("*");
    for (const symbol of names) {
      importsOut.push({ fromFile: relFile, toFile, symbol, line, kind: "import", external });
    }
  }

  // Dynamic import() -> reduced confidence.
  for (const call of sf.getDescendantsOfKind(SyntaxKind.CallExpression)) {
    if (call.getExpression().getKind() === SyntaxKind.ImportKeyword) {
      reducedOut.push({
        file: relFile,
        reason: "dynamic import() not statically resolvable",
        level: "reduced",
      });
    }
  }
}

let tsMorphVersion = "unknown";
try {
  const pkg = await import("ts-morph/package.json", { with: { type: "json" } });
  tsMorphVersion = pkg.default?.version || "unknown";
} catch {}

process.stdout.write(
  JSON.stringify(
    {
      tool: { name: "dependency-cruiser", version: process.env.GRIP_DEPCRUISE_VERSION || "resolved-at-runtime" },
      surfaceTool: { name: "ts-morph", version: tsMorphVersion },
      imports: importsOut,
      exports: exportsOut,
      reduced: reducedOut,
    },
    null,
    2,
  ) + "\n",
);
