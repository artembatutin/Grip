// Package php is the PHP language deriver. It wraps deptrac (dependency graph)
// and a bundled nikic/php-parser helper (public surface + call/extends edges,
// reflection/variable-variable confidence) via the "php" helper, and normalizes
// the resulting AnalyzerReport into the SAME Common Graph IR schema as TS — the
// proof the IR is genuinely language-neutral (D2).
package php

import (
	"context"

	"github.com/artembatutin/grip/internal/derive"
	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
)

// Language is the config/IR language key this deriver owns.
const Language = "php"

type deriver struct{}

// New returns the PHP deriver.
func New() derive.Deriver { return deriver{} }

func (deriver) Language() string { return Language }

func (deriver) Derive(ctx context.Context, spec plane.LanguageSpec, svc plane.DeriveServices, moduleIDs []string) (*ir.Graph, error) {
	return derive.RunHelper(ctx, "php", Language, spec, svc, moduleIDs)
}
