SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c

GO ?= go
COVERAGE_MIN ?= 60.0
COVERAGE_DIR := .coverage
COVERAGE_PROFILE := $(COVERAGE_DIR)/coverage.out
GO_FILES := $(shell rg --files -g '*.go' .)
RIGOR := $(GO) run ./scripts/rigor/cmd/rigor

.PHONY: help tools fmt fmt-check lint vet test race coverage coverage-threshold \
	fuzz-smoke benchmark-smoke verify-performance govulncheck dependency-inventory license-inventory \
	fuzz-smoke-regression \
	suppression-check \
	api-refresh api-boundary traceability-refresh schema-freshness generated-refresh \
	adapters conformance-check atlas-check workflow-validate hooks-install hooks-pre-commit-dry-run \
	hooks-pre-push-dry-run fast verify ci release-contract release-ga-contract release-dry-run clean \
	queue-me-check

help:
	@printf '%s\n' \
		'Available targets:' \
		'  make fast                  # local fast gate (fmt, lint, vet, API/schema checks)' \
		'  make verify                # full local verification suite' \
		'  make ci                    # canonical local CI entrypoint' \
		'  make generated-refresh     # refresh tracked rigor inventories'

tools:
	./scripts/rigor/install-tools.sh golangci-lint actionlint govulncheck

fmt:
	@if [[ -n "$(GO_FILES)" ]]; then \
		gofmt -w $(GO_FILES); \
	fi

fmt-check:
	@if [[ -z "$(GO_FILES)" ]]; then \
		exit 0; \
	fi
	@test -z "$$(gofmt -l $(GO_FILES))" || { \
		gofmt -l $(GO_FILES); \
		exit 1; \
	}

lint:
	./scripts/rigor/install-tools.sh golangci-lint
	./.cache/rigor/bin/golangci-lint run ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

race:
	CGO_ENABLED=1 $(GO) test -race ./...

coverage:
	mkdir -p $(COVERAGE_DIR)
	./scripts/rigor/check-coverage.sh $(COVERAGE_MIN) $(COVERAGE_PROFILE)

coverage-threshold: coverage

fuzz-smoke:
	./scripts/rigor/run-fuzz-smoke.sh

fuzz-smoke-regression:
	./scripts/rigor/run-fuzz-smoke.test.sh

benchmark-smoke:
	$(GO) test -run '^$$' -bench . -benchtime=1x ./...
	$(GO) run ./cmd/stave-performance -samples=11

verify-performance:
	mkdir -p .artifacts/performance
	$(GO) run ./cmd/stave-performance -strict -samples=$${STAVE_PERF_SAMPLES:-101} -out .artifacts/performance/report.json
	cd adapters/ssh && $(GO) test -run '^TestSSHTransportPerformanceBudget$$' -count=1 -v

govulncheck:
	./scripts/rigor/install-tools.sh govulncheck
	./.cache/rigor/bin/govulncheck ./...
	@for module in adapters/*/go.mod scripts/rigor/workflow-guard/go.mod; do \
		module_dir="$${module%/go.mod}"; \
		printf 'scanning nested module %s\n' "$$module_dir"; \
		(cd "$$module_dir" && "$(CURDIR)/.cache/rigor/bin/govulncheck" ./...) || exit "$$?"; \
	done

dependency-inventory:
	./scripts/rigor/check-generated.sh dependency-inventory

license-inventory:
	./scripts/rigor/check-generated.sh license-inventory

suppression-check:
	./scripts/rigor/check-suppressions.sh

api-refresh:
	./scripts/rigor/refresh-generated.sh public-api

api-boundary:
	$(RIGOR) boundary-check
	./scripts/rigor/check-generated.sh public-api

traceability-refresh:
	./scripts/rigor/refresh-generated.sh traceability

schema-freshness:
	./scripts/rigor/check-generated.sh traceability

generated-refresh:
	./scripts/rigor/refresh-generated.sh all

adapters:
	./scripts/rigor/check-adapters.sh

conformance-check:
	$(GO) run ./cmd/stave-conformance

atlas-check:
	$(GO) test ./internal/atlasrig ./cmd/atlas -count=1
	$(GO) run ./cmd/atlas -verify

queue-me-check:
	$(GO) test ./requirements -run '^TestQueueMe' -count=1

workflow-validate: queue-me-check
	./scripts/rigor/check-workflows.sh

hooks-install:
	find .githooks scripts/rigor -type f \( -name '*.sh' -o -name 'pre-commit' -o -name 'pre-push' \) -exec chmod +x {} +
	git config --local core.hooksPath .githooks

hooks-pre-commit-dry-run:
	./.githooks/pre-commit --dry-run

hooks-pre-push-dry-run:
	./.githooks/pre-push --dry-run

fast: fmt-check lint vet suppression-check api-boundary schema-freshness

verify: fast test race coverage-threshold fuzz-smoke-regression fuzz-smoke benchmark-smoke verify-performance dependency-inventory license-inventory adapters conformance-check atlas-check

release-contract:
	STAVE_CANDIDATE_GATE=1 $(GO) test ./requirements -count=1

release-ga-contract:
	STAVE_GA_RELEASE_GATE=1 $(GO) test ./requirements -count=1

ci: verify govulncheck workflow-validate release-contract

release-dry-run:
	./scripts/rigor/release-dry-run.sh

clean:
	rm -rf $(COVERAGE_DIR) .cache/rigor/tmp
