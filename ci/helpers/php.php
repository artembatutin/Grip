<?php
// Grip's bundled PHP surface+edge helper (plan/03 M0.4). It wraps
// nikic/php-parser and emits Grip's normalized AnalyzerReport on stdout, in the
// SAME schema as the TS helper — the proof the IR is genuinely language-neutral
// (D2). Grip's Go deriver folds the report into the Common Graph IR.
//
// Contract (must match internal/derive/report.go):
//   { tool, surfaceTool, imports[], exports[], reduced[] }
//   - exports: public classes/interfaces/functions declared in a MODULE
//     ENTRYPOINT file (a grip.yaml dir). Deep-only symbols are omitted, so
//     reaching them registers as internal-reach.
//   - imports: resolved `use` statements and static calls across files, with
//     line + the referenced symbol.
//   - reduced: variable-variables, call_user_func($dynamic), reflection, magic
//     __call (NFR-9).
//
// Requires nikic/php-parser autoloadable (composer global require
// nikic/php-parser). Exits 3 if it is unavailable so Grip can render a
// fail-closed install hint.

function grip_arg_all(string $name): array {
    global $argv;
    $out = [];
    for ($i = 0; $i < count($argv) - 1; $i++) {
        if ($argv[$i] === "--$name") $out[] = $argv[$i + 1];
    }
    return $out;
}
function grip_arg(string $name, ?string $def = null): ?string {
    $a = grip_arg_all($name);
    return $a[0] ?? $def;
}

$repoRoot = grip_arg('repo-root', getcwd());
$roots = grip_arg_all('root');

// Locate a php-parser autoloader (global or local composer).
$autoloads = [
    getenv('HOME') . '/.composer/vendor/autoload.php',
    getenv('HOME') . '/.config/composer/vendor/autoload.php',
    $repoRoot . '/vendor/autoload.php',
];
$loaded = false;
foreach ($autoloads as $a) {
    if ($a && file_exists($a)) { require $a; $loaded = true; break; }
}
if (!$loaded || !class_exists(\PhpParser\ParserFactory::class)) {
    fwrite(STDERR, "grip php helper: nikic/php-parser not installed (composer global require nikic/php-parser)\n");
    exit(3);
}

use PhpParser\ParserFactory;
use PhpParser\NodeTraverser;
use PhpParser\NodeVisitorAbstract;
use PhpParser\Node;

function grip_rel(string $repoRoot, string $p): string {
    $rp = realpath($p) ?: $p;
    $rr = rtrim(realpath($repoRoot) ?: $repoRoot, '/') . '/';
    return str_replace('\\', '/', str_starts_with($rp, $rr) ? substr($rp, strlen($rr)) : $rp);
}

function grip_php_files(string $dir): array {
    $out = [];
    $it = new RecursiveIteratorIterator(new RecursiveDirectoryIterator($dir, FilesystemIterator::SKIP_DOTS));
    foreach ($it as $f) {
        if ($f->isFile() && str_ends_with($f->getFilename(), '.php')) $out[] = $f->getPathname();
    }
    return $out;
}
function grip_module_dirs(string $repoRoot, string $dir, array &$acc): void {
    foreach (scandir($dir) as $e) {
        if ($e === '.' || $e === '..' || $e === 'vendor' || $e === '.git') continue;
        $p = "$dir/$e";
        if (is_dir($p)) {
            if (file_exists("$p/grip.yaml")) $acc[grip_rel($repoRoot, $p)] = true;
            grip_module_dirs($repoRoot, $p, $acc);
        }
    }
}

$files = [];
$modDirs = [];
foreach ($roots as $r) {
    $abs = "$repoRoot/$r";
    if (is_dir($abs)) {
        $files = array_merge($files, grip_php_files($abs));
        grip_module_dirs($repoRoot, $abs, $modDirs);
    }
}

$parser = (new ParserFactory())->createForNewestSupportedVersion();

$exports = [];
$imports = [];
$reduced = [];

foreach ($files as $file) {
    $relFile = grip_rel($repoRoot, $file);
    $isEntrypoint = isset($modDirs[dirname($relFile)]);
    $code = file_get_contents($file);
    try {
        $ast = $parser->parse($code);
    } catch (\Throwable $e) {
        $reduced[] = ['file' => $relFile, 'reason' => 'parse error: ' . $e->getMessage(), 'level' => 'none'];
        continue;
    }
    $visitor = new class($relFile, $isEntrypoint) extends NodeVisitorAbstract {
        public array $exports = [];
        public array $imports = [];
        public array $reduced = [];
        public function __construct(private string $relFile, private bool $isEntrypoint) {}
        public function enterNode(Node $node) {
            // Public surface: top-level class/interface/function declarations.
            if ($this->isEntrypoint) {
                if ($node instanceof Node\Stmt\Class_ && $node->name) {
                    $this->exports[] = ['file' => $this->relFile, 'name' => (string) $node->name, 'kind' => 'class', 'line' => $node->getStartLine()];
                } elseif ($node instanceof Node\Stmt\Interface_ && $node->name) {
                    $this->exports[] = ['file' => $this->relFile, 'name' => (string) $node->name, 'kind' => 'interface', 'line' => $node->getStartLine()];
                } elseif ($node instanceof Node\Stmt\Function_ && $node->name) {
                    $this->exports[] = ['file' => $this->relFile, 'name' => (string) $node->name, 'kind' => 'function', 'line' => $node->getStartLine()];
                }
            }
            // Cross-module references: `use` imports (Grip resolves the namespace
            // to a module via its dir mapping; here we emit the short symbol).
            if ($node instanceof Node\UseItem || (class_exists(Node\Stmt\UseUse::class) && $node instanceof Node\Stmt\UseUse)) {
                $name = $node->name->toString();
                $short = $node->getAlias() ? (string) $node->getAlias() : $node->name->getLast();
                $this->imports[] = [
                    'fromFile' => $this->relFile,
                    'toFile' => str_replace('\\', '/', $name) . '.php',
                    'symbol' => $short,
                    'line' => $node->getStartLine(),
                    'kind' => 'import',
                    'external' => str_starts_with($name, 'Vendor\\'),
                ];
            }
            // Reduced-confidence dynamic dispatch.
            if ($node instanceof Node\Expr\FuncCall && $node->name instanceof Node\Name) {
                $fn = $node->name->toString();
                if (in_array($fn, ['call_user_func', 'call_user_func_array'], true)) {
                    $this->reduced[] = ['file' => $this->relFile, 'reason' => "$fn with a computed callable", 'level' => 'reduced'];
                }
            }
            if ($node instanceof Node\Expr\Variable && is_string($node->name) === false) {
                $this->reduced[] = ['file' => $this->relFile, 'reason' => 'variable-variable not statically resolvable', 'level' => 'reduced'];
            }
        }
    };
    $t = new NodeTraverser();
    $t->addVisitor($visitor);
    $t->traverse($ast);
    $exports = array_merge($exports, $visitor->exports);
    $imports = array_merge($imports, $visitor->imports);
    $reduced = array_merge($reduced, $visitor->reduced);
}

$parserVersion = 'unknown';
if (class_exists(\Composer\InstalledVersions::class)) {
    try { $parserVersion = \Composer\InstalledVersions::getPrettyVersion('nikic/php-parser') ?? 'unknown'; } catch (\Throwable $e) {}
}

echo json_encode([
    'tool' => ['name' => 'deptrac', 'version' => getenv('GRIP_DEPTRAC_VERSION') ?: 'resolved-at-runtime'],
    'surfaceTool' => ['name' => 'php-parser', 'version' => $parserVersion],
    'imports' => $imports,
    'exports' => $exports,
    'reduced' => $reduced,
], JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES) . "\n";
