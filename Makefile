# Grip — build, verify, and acceptance targets (plan/03 M0.0, M0.11).
# `make check` is the pre-commit bar: build + vet + fmt + lint + test.
# `make acceptance` runs the end-to-end M0 gate over the fixture matrix.

GO        ?= go
BIN       ?= bin/grip
PKG       := ./...
# The module cache in some sandboxes is read-only; disabling the sumdb avoids a
# spurious network write. Real environments can drop this.
GOENV     := GOFLAGS=-mod=mod GOSUMDB=off

.PHONY: all build check fmt vet lint test acceptance determinism cover clean tidy install

all: check

build:
	$(GOENV) $(GO) build -trimpath -o $(BIN) ./cmd/grip
	@echo "built $(BIN)"

# Static single binary (CGO off), the D1 distribution shape.
install:
	CGO_ENABLED=0 $(GOENV) $(GO) install -trimpath ./cmd/grip

check: build vet fmt-check lint test
	@echo "make check: OK"

fmt:
	$(GO) fmt $(PKG)

fmt-check:
	@out="$$(gofmt -l cmd internal)"; \
	if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi
	@echo "gofmt: clean"

vet:
	$(GOENV) $(GO) vet $(PKG)

# golangci-lint is optional: run it if installed, otherwise skip with a note
# (CI installs it; local `make check` should not hard-fail without it).
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./... ; \
	else \
		echo "golangci-lint not installed — skipping (install to match CI)"; \
	fi

test:
	$(GOENV) $(GO) test $(PKG)

# The M0 exit gate: the full acceptance matrix over the PHP+TS fixtures.
acceptance:
	$(GOENV) $(GO) test -count=1 -run 'TestAcceptanceMatrix|TestDeterminism|TestMergedIR' ./internal/acceptance/ -v

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
