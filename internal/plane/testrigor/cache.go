package testrigor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/artembatutin/grip/internal/vcs"
)

// Mutation testing is slow, so test-rigor caches a module's derived state keyed by
// a CONTENT hash (module id + sorted file bytes + tool name + tool version). This
// gives the plan's "full in CI / changed-only locally" behavior from one code
// path with no mode flag: a cold cache (fresh CI checkout) recomputes everything;
// a warm cache recomputes only modules whose content changed. Determinism is
// preserved structurally — the key is pure content, never a clock or path — so a
// cache hit and a fresh compute for the same content are byte-identical (asserted
// in tests). The cache is an OPTIMIZATION and never changes the gate decision.

// Cache stores derived module states by content-hash key. It is injected so tests
// use an in-memory implementation and production a filesystem one.
type Cache interface {
	Get(key string) (*ModuleState, bool)
	Put(key string, st *ModuleState)
}

// NewCacheFunc builds a Cache for a repo root. The plane calls it inside Derive
// (where the root is known via DeriveServices), keeping construction lazy.
type NewCacheFunc func(repoRoot string) Cache

// contentHash is the cache key: stable across runs and machines for identical
// content and tool version. Missing files are folded in as an explicit marker so
// a deleted file changes the hash rather than being silently skipped.
func contentHash(repoRoot, moduleID string, files []string, toolName, toolVersion string) string {
	h := sha256.New()
	writeField(h, "module", moduleID)
	writeField(h, "tool", toolName)
	writeField(h, "version", toolVersion)
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	for _, f := range sorted {
		writeField(h, "file", f)
		b, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(f)))
		if err != nil {
			writeField(h, "missing", f)
			continue
		}
		h.Write([]byte{0})
		h.Write(b)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeField(h interface{ Write([]byte) (int, error) }, k, v string) {
	_, _ = h.Write([]byte(k))
	_, _ = h.Write([]byte{0x1f})
	_, _ = h.Write([]byte(v))
	_, _ = h.Write([]byte{0x1e})
}

// changedModules returns the set of module ids that touch a file changed vs the
// git baseline (working tree vs HEAD), and whether the changed set is known. When
// git cannot answer (no repo, offline harness), ok is false and callers fall back
// to the content hash alone — a cold full derive is always correct.
func changedModules(ctx context.Context, repoRoot string, moduleOf func(string) string) (set map[string]bool, ok bool) {
	files, have := vcs.ChangedFiles(ctx, repoRoot, "")
	if !have {
		return nil, false
	}
	set = map[string]bool{}
	for _, f := range files {
		if moduleOf == nil {
			continue
		}
		if id := moduleOf(f); id != "" {
			set[id] = true
		}
	}
	return set, true
}

// memoryCache is the in-memory Cache used by tests (and a safe default when no
// filesystem cache is wired). It clones on the way in and out so a stored state
// cannot be mutated after the fact.
type memoryCache struct {
	m map[string]*ModuleState
}

// NewMemoryCache returns an empty in-memory cache.
func NewMemoryCache() Cache { return &memoryCache{m: map[string]*ModuleState{}} }

func (c *memoryCache) Get(key string) (*ModuleState, bool) {
	st, ok := c.m[key]
	if !ok {
		return nil, false
	}
	return cloneState(st), true
}

func (c *memoryCache) Put(key string, st *ModuleState) { c.m[key] = cloneState(st) }

// fsCache persists states as JSON under <repoRoot>/.grip-cache/testrigor (a
// directory module discovery ignores, so cache files are never mistaken for
// source). A corrupt or unreadable entry is treated as a miss — the module simply
// recomputes, never a wrong result.
type fsCache struct {
	dir string
}

func newFSCache(repoRoot string) Cache {
	return &fsCache{dir: filepath.Join(repoRoot, ".grip-cache", "testrigor")}
}

func (c *fsCache) Get(key string) (*ModuleState, bool) {
	b, err := os.ReadFile(filepath.Join(c.dir, key+".json"))
	if err != nil {
		return nil, false
	}
	var st ModuleState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, false
	}
	return &st, true
}

func (c *fsCache) Put(key string, st *ModuleState) {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return // caching is best-effort; a write failure just means a future miss.
	}
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(c.dir, key+".json"), b, 0o644)
}

func cloneState(st *ModuleState) *ModuleState {
	if st == nil {
		return nil
	}
	cp := *st
	cp.Tests = append([]TestState(nil), st.Tests...)
	for i := range cp.Tests {
		cp.Tests[i].Behaviors = append([]string(nil), st.Tests[i].Behaviors...)
	}
	return &cp
}
