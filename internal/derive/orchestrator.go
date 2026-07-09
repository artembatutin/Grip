package derive

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
)

// Orchestrator runs the registered language derivers concurrently and merges
// their per-language graphs into one Common Graph IR (D2). It is the only place
// that fans out to languages; the engine above it sees a single IR.
type Orchestrator struct {
	derivers map[string]Deriver
}

// NewOrchestrator builds an orchestrator from a set of language derivers.
func NewOrchestrator(derivers ...Deriver) *Orchestrator {
	o := &Orchestrator{derivers: map[string]Deriver{}}
	for _, d := range derivers {
		o.derivers[d.Language()] = d
	}
	return o
}

// Derive groups the governed modules by language, runs each language's deriver
// concurrently (wall-clock ≈ slowest deriver, NFR-4), and merges the results.
// A configured language with governed modules but no registered deriver is a
// fail-closed error, never a silent skip.
func (o *Orchestrator) Derive(ctx context.Context, mods []plane.ModuleRef, svc plane.DeriveServices) (*ir.Graph, error) {
	byLang := map[string][]string{}
	for _, m := range mods {
		byLang[m.Language] = append(byLang[m.Language], m.ID)
	}
	for lang := range byLang {
		sort.Strings(byLang[lang])
	}

	// Determine the language processing order deterministically.
	langs := make([]string, 0, len(svc.Languages))
	for _, spec := range svc.Languages {
		langs = append(langs, spec.Language)
	}
	sort.Strings(langs)
	specByLang := map[string]plane.LanguageSpec{}
	for _, spec := range svc.Languages {
		specByLang[spec.Language] = spec
	}

	// Derivers for different languages run concurrently; wall-clock ≈ slowest
	// deriver (NFR-4). The first error wins and cancels the rest.
	graphs := make([]*ir.Graph, len(langs))
	errs := make([]error, len(langs))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for i, lang := range langs {
		spec := specByLang[lang]
		ids := byLang[lang]
		d, ok := o.derivers[lang]
		if !ok {
			if len(ids) > 0 {
				return nil, fmt.Errorf("no deriver registered for language %q (has %d governed modules)", lang, len(ids))
			}
			continue
		}
		wg.Add(1)
		go func(i int, spec plane.LanguageSpec, ids []string) {
			defer wg.Done()
			out, err := d.Derive(ctx, spec, svc, ids)
			if err != nil {
				errs[i] = err
				cancel()
				return
			}
			graphs[i] = out
		}(i, spec, ids)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return ir.Merge(svc.Commit, graphs...)
}
