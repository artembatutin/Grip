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
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";

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

function executable(name) {
  const local = join(repoRoot, "node_modules", ".bin", name);
  if (existsSync(local)) return local;
  for (const dir of (process.env.PATH || "").split(":")) {
    const candidate = join(dir, name);
    if (existsSync(candidate)) return candidate;
  }
  return null;
}

// dependency-cruiser is deliberately executed, not merely attributed. Its
// complete JSON graph gives the resolver an independent check over the source
// set before ts-morph provides symbol-level evidence below.
const depcruise = executable("depcruise");
if (!depcruise) {
  process.stderr.write("grip ts helper: dependency-cruiser not installed (npm i -D dependency-cruiser ts-morph)\n");
  process.exit(3);
}
const depVersionRun = spawnSync(depcruise, ["--version"], { cwd: repoRoot, encoding: "utf8" });
if (depVersionRun.status !== 0 || !depVersionRun.stdout.trim()) {
  process.stderr.write(`grip ts helper: cannot determine dependency-cruiser version: ${depVersionRun.stderr}\n`);
  process.exit(3);
}
const depcruiseConfig = [".dependency-cruiser.js", ".dependency-cruiser.cjs", ".dependency-cruiser.mjs", ".dependency-cruiser.json"].some((name) => existsSync(join(repoRoot, name)));
const cruiseArgs = ["--output-type", "json"];
if (!depcruiseConfig) cruiseArgs.push("--no-config");
cruiseArgs.push(...roots);
const cruise = spawnSync(depcruise, cruiseArgs, { cwd: repoRoot, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
if (cruise.status !== 0) {
  process.stderr.write(`grip ts helper: dependency-cruiser failed: ${cruise.stderr}\n`);
  process.exit(2);
}
try {
  const graph = JSON.parse(cruise.stdout);
  if (!Array.isArray(graph.modules)) throw new Error("missing modules array");
} catch (e) {
  process.stderr.write(`grip ts helper: malformed dependency-cruiser JSON: ${e.message}\n`);
  process.exit(2);
}

let Project, SyntaxKind, ModuleResolutionKind, ts, tsMorphRequire;
try {
  let mod;
  try {
    tsMorphRequire = createRequire(join(repoRoot, "package.json"));
    mod = tsMorphRequire("ts-morph");
  } catch {
    try {
      // When dependency-cruiser came from a project-local installation, its
      // .bin directory resolves siblings from that same node_modules tree.
      tsMorphRequire = createRequire(join(dirname(dirname(depcruise)), "..", "package.json"));
      mod = tsMorphRequire("ts-morph");
    } catch {
      const npm = spawnSync("npm", ["root", "-g"], { cwd: repoRoot, encoding: "utf8" });
      if (npm.status !== 0) throw new Error("npm global root unavailable");
      tsMorphRequire = createRequire(join(npm.stdout.trim(), "package.json"));
      mod = tsMorphRequire("ts-morph");
    }
  }
  ({ Project, SyntaxKind, ModuleResolutionKind, ts } = mod);
} catch (e) {
  process.stderr.write("grip ts helper: ts-morph not installed (npm i -D ts-morph)\n");
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

function findTsConfig(dir) {
  const direct = join(dir, "tsconfig.json");
  if (existsSync(direct)) return direct;
  for (const e of readdirSync(dir)) {
    if (e === "node_modules" || e === ".git") continue;
    const p = join(dir, e);
    if (statSync(p).isDirectory()) {
      const nested = findTsConfig(p);
      if (nested) return nested;
    }
  }
  return null;
}
const tsconfig = roots.map((r) => findTsConfig(join(repoRoot, r))).find(Boolean);
const project = tsconfig
  ? new Project({ tsConfigFilePath: tsconfig, skipAddingFilesFromTsConfig: false })
  : new Project({ compilerOptions: { allowJs: true, moduleResolution: ModuleResolutionKind.Bundler }, skipAddingFilesFromTsConfig: true });
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

  // Static CommonJS require() calls carry the same architectural authority as
  // ES imports. Computed requires remain explicit reduced-confidence evidence.
  for (const call of sf.getDescendantsOfKind(SyntaxKind.CallExpression)) {
    if (call.getExpression().getText() !== "require") continue;
    const arg0 = call.getArguments()[0];
    if (!arg0 || !arg0.asKind(SyntaxKind.StringLiteral)) {
      reducedOut.push({ file: relFile, reason: "computed CommonJS require() not statically resolvable", level: "reduced" });
      continue;
    }
    const specifier = arg0.getLiteralText();
    const resolved = ts.resolveModuleName(specifier, sf.getFilePath(), project.getCompilerOptions(), ts.sys).resolvedModule?.resolvedFileName;
    importsOut.push({ fromFile: relFile, toFile: resolved ? rel(resolved) : specifier, symbol: "*", line: call.getStartLineNumber(), kind: "require", external: !resolved });
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
  const pkgPath = tsMorphRequire.resolve("ts-morph/package.json");
  tsMorphVersion = tsMorphRequire(pkgPath).version || "unknown";
} catch {}

importsOut.sort((a, b) => `${a.fromFile}\0${a.toFile}\0${a.symbol}\0${a.line}`.localeCompare(`${b.fromFile}\0${b.toFile}\0${b.symbol}\0${b.line}`));
exportsOut.sort((a, b) => `${a.file}\0${a.name}\0${a.line}`.localeCompare(`${b.file}\0${b.name}\0${b.line}`));
reducedOut.sort((a, b) => `${a.file}\0${a.reason}`.localeCompare(`${b.file}\0${b.reason}`));

process.stdout.write(
  JSON.stringify(
    {
      tool: { name: "dependency-cruiser", version: depVersionRun.stdout.trim().replace(/^v/, "") },
      surfaceTool: { name: "ts-morph", version: tsMorphVersion },
      imports: importsOut,
      exports: exportsOut,
      reduced: reducedOut,
    },
    null,
    2,
  ) + "\n",
);
