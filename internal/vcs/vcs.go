// Package vcs is thin git plumbing: the resolved commit for IR identity and the
// changed-file set for incremental local gating (plan/01 §7). It degrades
// gracefully outside a git repo or before the first commit, because a fresh repo
// must still gate (M0.10 onboarding).
package vcs

import (
	"context"
	"os/exec"
	"strings"
)

// HeadCommit returns the resolved HEAD commit, or "uncommitted" when the repo has
// no commits yet, or "no-vcs" when there is no git at all. The IR carries this
// only as identity — determinism is over the derived structure, not the commit.
func HeadCommit(ctx context.Context, repoRoot string) string {
	out, err := run(ctx, repoRoot, "rev-parse", "HEAD")
	if err != nil {
		if isGitRepo(ctx, repoRoot) {
			return "uncommitted"
		}
		return "no-vcs"
	}
	return strings.TrimSpace(out)
}

// ChangedFiles returns repo-relative paths that differ from base (or from HEAD
// plus the working tree when base is empty). An empty result with ok=false means
// "could not determine — derive everything" (fail-open on the optimization, not
// on the decision: a cold full derive is always correct).
func ChangedFiles(ctx context.Context, repoRoot, base string) (files []string, ok bool) {
	ref := base
	if ref == "" {
		ref = "HEAD"
	}
	out, err := run(ctx, repoRoot, "diff", "--name-only", ref)
	if err != nil {
		return nil, false
	}
	untracked, _ := run(ctx, repoRoot, "ls-files", "--others", "--exclude-standard")
	set := map[string]bool{}
	for _, line := range append(splitLines(out), splitLines(untracked)...) {
		if line != "" {
			set[line] = true
		}
	}
	for f := range set {
		files = append(files, f)
	}
	return files, true
}

func isGitRepo(ctx context.Context, repoRoot string) bool {
	_, err := run(ctx, repoRoot, "rev-parse", "--is-inside-work-tree")
	return err == nil
}

func run(ctx context.Context, repoRoot string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	return string(out), err
}

func splitLines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}
