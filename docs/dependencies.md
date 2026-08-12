# Dependency Policy

## Policy

Stave core stays standard-library-first. Optional integrations may accelerate adapters, but they must not define the core compatibility contract.

## Rules

1. Prefer the Go standard library in the root module.
2. Generated Unicode width tables are allowed only when the source, version, and regeneration path are recorded.
3. Dependencies for security-sensitive parsing, terminal handling, or width behavior require an ADR and conformance tests.
4. No third-party dependency type may appear in a public core signature.
5. Optional integrations belong in nested modules.
6. Dependencies must be pinned, checksummed, vulnerability-scanned, and licence-reviewed.
7. Avoid packages with explicit stability disclaimers in compatibility-defining paths.
8. Core never imports Lopper.
9. Applications may compose Bubble Tea or Lip Gloss adapters without forcing other consumers to do the same.
10. Do not add dependencies only for convenience when a small deterministic local implementation is sufficient.

## Review checklist for a new dependency

| Check | Owner | Proof command |
|---|---|---|
| Dependency is outside core if it carries renderer/runtime churn risk | Architecture owner | `make adapters` and rigor boundary check |
| Version pin, checksum, and licence review are recorded | Release owner | `make dependency-inventory license-inventory` |
| Security-sensitive dependency has an ADR | Security owner | `docs/adr/` review |
| Conformance or regression tests cover the adopted behavior | Test owner | `make adapters` and package tests |

## Allowed dependency classes

| Class | Policy |
|---|---|
| Standard library | Preferred for root core |
| Generated data tables | Allowed with pinned source and regeneration proof |
| Adapter-only UI frameworks | Allowed in nested modules only |
| Security-sensitive parsers or terminal libraries | Allowed only with ADR plus conformance suite |
| Observability helpers | Allowed only if secrets remain redacted and injection boundaries hold |

## Related documents

- [Architecture Overview](architecture.md)
- [Compatibility, Versioning, and Schema Policy](compatibility.md)
- [ADR-001](adr/ADR-001.md)
- [ADR-015](adr/ADR-015.md)
