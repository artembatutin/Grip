# Grip — build, verify, and acceptance targets (plan/03 M0.0, M0.11).
# `make check` is the pre-commit bar: build + vet + fmt + lint + test.
# `make acceptance` runs the end-to-end M0 gate over the fixture matrix.

GO        ?= go
BIN       ?= bin/grip
VERSION   ?= devel
GOLANGCI_LINT_VERSION ?= v2.12.2
PKG       := ./...
# The module cache in some sandboxes is read-only; disabling the sumdb avoids a
# spurious network write. Real environments can drop this.
GOENV     := GOFLAGS=-mod=mod GOSUMDB=off

.PHONY: all build check fmt vet lint test acceptance determinism dogfood proof cover clean tidy install

all: check

build:
	$(GOENV) $(GO) build -trimpath -ldflags "-X github.com/artembatutin/grip/internal/cli.Version=$(VERSION)" -o $(BIN) ./cmd/grip
	@echo "built $(BIN)"

# Static single binary (CGO off), the D1 distribution shape.
install:
	CGO_ENABLED=0 $(GOENV) $(GO) install -trimpath -ldflags "-X github.com/artembatutin/grip/internal/cli.Version=$(VERSION)" ./cmd/grip

check: build vet fmt-check lint test dogfood
	@echo "make check: OK"

fmt:
	$(GO) fmt $(PKG)

fmt-check:
	@out="$$(gofmt -l cmd internal)"; \
	if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi
	@echo "gofmt: clean"

vet:
	$(GOENV) $(GO) vet $(PKG)

# Match CI exactly even when golangci-lint is not installed locally. `go run`
# caches the pinned tool without adding it to this module's dependencies.
lint:
	$(GOENV) $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout=5m ./...

test:
	$(GOENV) $(GO) test $(PKG)

# The M0 exit gate: the full acceptance matrix, including real Go derivation.
acceptance:
	$(GOENV) $(GO) test -count=1 -run 'TestAcceptanceMatrix|TestGo|TestDeterminism|TestMergedIR' ./internal/acceptance/ -v

# Build the candidate binary, then make Grip govern Grip's own package graph.
# After the first Go-capable release, CI should additionally run that pinned
# release as the guardian so a PR-built binary is never its sole judge.
dogfood: build
	./$(BIN) gate --ci

# Reproduce the full adversarial proof across all four planes, then self-gate.
proof: build
	$(GOENV) $(GO) test -count=1 -run 'TestGo' ./internal/acceptance/ -v
	./$(BIN) gate --ci
	./$(BIN) diff
	@first="$$(mktemp)"; second="$$(mktemp)"; \
	trap 'rm -f "$$first" "$$second"' 0; \
	GRIP_COMMIT=ci-proof ./$(BIN) gate --ci --json > "$$first"; \
	GRIP_COMMIT=ci-proof ./$(BIN) gate --ci --json > "$$second"; \
	cmp -s "$$first" "$$second" || { diff -u "$$first" "$$second"; exit 1; }; \
	echo "grip: deterministic four-plane JSON report"

# Determinism proof in isolation (100x IR-hash stability).
determinism:
	$(GOENV) $(GO) test -count=1 -run TestDeterminismIRHash ./internal/acceptance/ -v

cover:
	$(GOENV) $(GO) test -coverprofile=coverage.out $(PKG)
	$(GO) tool cover -func=coverage.out | tail -1

tidy:
	$(GOENV) $(GO) mod tidy

clean:
	rm -rf bin coverage.out
