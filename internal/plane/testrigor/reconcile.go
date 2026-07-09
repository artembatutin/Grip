package testrigor

import (
	"sort"

	"github.com/artembatutin/grip/internal/ir"
	"github.com/artembatutin/grip/internal/plane"
)

// reconcile is the pure heart of the test-rigor plane: (declared intents, derived
// model) → located, one-sentence violations. No I/O, no clock, no map-iteration
// leaks — every loop is over a sorted key set, so output is byte-identical under
// shuffled inputs (NFR-1, principle 3). Flaky signals that touch a rule become
// fail-closed cannot-verify results rather than being trusted (never a false
// pass). It mirrors the architecture plane's reconcile in discipline while
// reading an entirely non-graph model — the proof the seam generalizes.
func reconcile(intents map[string]Intent, m *Model) []plane.Violation {
	if m == nil {
		return nil
	}
	var vs []plane.Violation

	ids := make([]string, 0, len(intents))
	for id := range intents {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		intent := intents[id]
		if !intent.HasSection {
			continue // opt-in: a module with no testRigor section makes no claims.
		}
		st := m.Module(id)
		behaviors := append([]string(nil), intent.RequiredBehaviors...)
		sort.Strings(behaviors)

		// --- Fail-closed first (NFR-9): a flaky signal touching a governed rule
		// blocks rather than being counted — a flaky test's kill signal is not
		// trustworthy, so trusting it risks a false pass.
		if st != nil && intent.BoundaryContract && st.ContractFlaky {
			vs = append(vs, plane.Violation{
				RuleID: RuleVacuousContract, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindCannotVerify,
				Location:   plane.Location{Module: id, File: st.ContractFile, Line: st.ContractLine, Symbol: st.ContractTestID},
				Confidence: ir.LevelReduced,
				Message:    msgFlakyContract(id, st.ContractTestID, ir.LevelReduced),
			})
		}
		if st != nil {
			for _, b := range behaviors {
				cov := coveringTests(st, b)
				if len(cov) == 0 {
					continue
				}
				if allFlaky(cov) {
					t := cov[0]
					vs = append(vs, plane.Violation{
						RuleID: RuleSkippedRequiredTest, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindCannotVerify,
						Location:   plane.Location{Module: id, File: t.File, Line: t.Line, Symbol: t.ID},
						Confidence: ir.LevelReduced,
						Message:    msgFlakyRequired(id, b, t.ID, ir.LevelReduced),
					})
				}
			}
		}

		// --- Tier A: vacuous boundary contract. A declared contract test that
		// exists, is trustworthy (not flaky), has mutants, yet kills none of them.
		if st != nil && intent.BoundaryContract && st.ContractPresent && !st.ContractFlaky &&
			st.ContractMutants > 0 && st.ContractKilled == 0 {
			vs = append(vs, plane.Violation{
				RuleID: RuleVacuousContract, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindViolation,
				Location:   plane.Location{Module: id, File: st.ContractFile, Line: st.ContractLine, Symbol: st.ContractTestID},
				Confidence: ir.LevelFull,
				Message:    msgVacuousContract(id, st.ContractTestID, st.ContractFile, st.ContractLine, st.ContractMutants),
			})
		}

		// --- Tier A: deleted / skipped required test (behavior-by-behavior).
		bl := baselineFor(m, id)
		for _, b := range behaviors {
			cov := coveringTests(st, b)

			// deleted-required-test: a baseline test for this still-required
			// behavior is gone. (Dropping the behavior from the manifest is an
			// intentional edit — we only iterate CURRENT required behaviors.)
			if bl != nil {
				have := testIDSet(cov)
				baseTests := append([]string(nil), bl.RequiredTests[b]...)
				sort.Strings(baseTests)
				for _, bt := range baseTests {
					if !have[bt] {
						vs = append(vs, plane.Violation{
							RuleID: RuleDeletedRequiredTest, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindViolation,
							Location:   plane.Location{Module: id, Symbol: b},
							Confidence: ir.LevelFull,
							Message:    msgDeletedRequiredTest(id, b, bt),
						})
					}
				}
			}

			// skipped-required-test: the behavior has trustworthy tests but every
			// one of them is skipped, so it is not actually exercised. (An all-flaky
			// behavior was already handled as cannot-verify above.)
			nonFlaky := nonFlakyTests(cov)
			if len(nonFlaky) > 0 && allSkipped(nonFlaky) {
				t := firstSkipped(nonFlaky)
				vs = append(vs, plane.Violation{
					RuleID: RuleSkippedRequiredTest, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindViolation,
					Location:   plane.Location{Module: id, File: t.File, Line: t.Line, Symbol: t.ID},
					Confidence: ir.LevelFull,
					Message:    msgSkippedRequiredTest(id, b, t.ID),
				})
			}
		}

		// --- Tier A: threshold tamper. A governed threshold lowered vs baseline.
		if intent.HasThreshold && bl != nil && bl.HasThreshold && intent.MutationThreshold < bl.Threshold {
			vs = append(vs, plane.Violation{
				RuleID: RuleThresholdTamper, Plane: PlaneID, Tier: plane.TierA, Kind: plane.KindViolation,
				Location:   plane.Location{Module: id},
				Confidence: ir.LevelFull,
				Message:    msgThresholdTamper(id, bl.Threshold, intent.MutationThreshold),
			})
		}

		// --- Tier B advisories (non-blocking, promotable): trends vs baseline.
		if st != nil && st.Analyzed && bl != nil {
			if bl.HasScore && st.MutationScore < bl.MutationScore {
				vs = append(vs, plane.Violation{
					RuleID: RuleDecliningMutation, Plane: PlaneID, Tier: plane.TierB, Kind: plane.KindViolation,
					Location:   plane.Location{Module: id},
					Confidence: ir.LevelFull,
					Message:    msgDecliningMutation(id, bl.MutationScore, st.MutationScore),
				})
			}
			if bl.HasMockRatio && st.MockRatio > bl.MockRatio {
				vs = append(vs, plane.Violation{
					RuleID: RuleRisingMockRatio, Plane: PlaneID, Tier: plane.TierB, Kind: plane.KindViolation,
					Location:   plane.Location{Module: id},
					Confidence: ir.LevelFull,
					Message:    msgRisingMockRatio(id, bl.MockRatio, st.MockRatio),
				})
			}
		}

		// --- Tier C report (never blocks): unearned trust made visible (GR-TST-3).
		// A module that hides internals (declares behaviors) but carries no verified
		// boundary contract.
		switch {
		case intent.BoundaryContract && (st == nil || !st.ContractPresent):
			vs = append(vs, plane.Violation{
				RuleID: RuleUnverifiedModule, Plane: PlaneID, Tier: plane.TierC, Kind: plane.KindViolation,
				Location:   plane.Location{Module: id},
				Confidence: ir.LevelFull,
				Message:    msgUnverifiedMissingContract(id),
			})
		case len(intent.RequiredBehaviors) > 0 && !intent.BoundaryContract:
			vs = append(vs, plane.Violation{
				RuleID: RuleUnverifiedModule, Plane: PlaneID, Tier: plane.TierC, Kind: plane.KindViolation,
				Location:   plane.Location{Module: id},
				Confidence: ir.LevelFull,
				Message:    msgUnverifiedModule(id),
			})
		}
	}

	return vs
}

func baselineFor(m *Model, id string) *BaselineState {
	if m.Baseline == nil {
		return nil
	}
	return m.Baseline[id]
}

// coveringTests returns the tests of a module that cover a behavior, in id order
// (st.Tests is already id-sorted by Model.index).
func coveringTests(st *ModuleState, behavior string) []TestState {
	if st == nil {
		return nil
	}
	var out []TestState
	for _, t := range st.Tests {
		for _, b := range t.Behaviors {
			if b == behavior {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

func nonFlakyTests(ts []TestState) []TestState {
	var out []TestState
	for _, t := range ts {
		if !t.Flaky {
			out = append(out, t)
		}
	}
	return out
}

func testIDSet(ts []TestState) map[string]bool {
	out := make(map[string]bool, len(ts))
	for _, t := range ts {
		out[t.ID] = true
	}
	return out
}

func allFlaky(ts []TestState) bool {
	for _, t := range ts {
		if !t.Flaky {
			return false
		}
	}
	return len(ts) > 0
}

func allSkipped(ts []TestState) bool {
	for _, t := range ts {
		if !t.Skipped {
			return false
		}
	}
	return len(ts) > 0
}

func firstSkipped(ts []TestState) TestState {
	for _, t := range ts {
		if t.Skipped {
			return t
		}
	}
	return TestState{}
}
