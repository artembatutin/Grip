// Package typescript is the TS/JS language deriver. It wraps dependency-cruiser
// (resolution) and a bundled ts-morph helper (imports/exports with line+symbol,
// dynamic-import confidence) via the "typescript" helper, and normalizes the
// resulting AnalyzerReport into the Common Graph IR. It adds no engine coupling:
// adding a language is exactly this file plus a helper script (D9).
package typescript

import (
	"context"

	"github.com/artembatutin/grip/internal/derive"
	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
)

// Language is the config/IR language key this deriver owns.
const Language = "typescript"

type deriver struct{}

// New returns the TypeScript/JS deriver.
func New() derive.Deriver { return deriver{} }

func (deriver) Language() string { return Language }

func (deriver) Derive(ctx context.Context, spec plane.LanguageSpec, svc plane.DeriveServices, moduleIDs []string) (*ir.Graph, error) {
	return derive.RunHelper(ctx, "typescript", Language, spec, svc, moduleIDs)
}
