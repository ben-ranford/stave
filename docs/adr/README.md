# ADR Index

These records capture the accepted architectural decisions from the Stave v1 system design. Each ADR carries a proof placeholder so the implementation can tie the decision to a concrete verification command.

- [ADR-001: Stave-native core; external TUI frameworks are adapters](ADR-001.md)
- [ADR-002: Immutable semantic tree is canonical](ADR-002.md)
- [ADR-003: Stable IDs derive from application-owned logical keys](ADR-003.md)
- [ADR-004: Target references include NodeID, generation, and observed revision](ADR-004.md)
- [ADR-005: Reducers and views are pure; all I/O is an effect](ADR-005.md)
- [ADR-006: One owner serializes reductions; effect outcomes are recorded events](ADR-006.md)
- [ADR-007: Rendering flows from semantic tree to layout plan to cell surface to sink](ADR-007.md)
- [ADR-008: Capabilities are negotiated immutable manifests with safe degradation](ADR-008.md)
- [ADR-009: Themes use semantic roles and are supplied by applications](ADR-009.md)
- [ADR-010: Human and agent commands resolve to the same typed action registry](ADR-010.md)
- [ADR-011: JSON-RPC 2.0 over JSONL stdio is the v1 agent transport](ADR-011.md)
- [ADR-012: Machine stdout carries protocol and output only; diagnostics use stderr or observers](ADR-012.md)
- [ADR-013: Secret values are opaque handles excluded from state artifacts](ADR-013.md)
- [ADR-014: Lopper adopts through an application-owned strangler adapter](ADR-014.md)
- [ADR-015: Root and optional adapters are separate Go modules](ADR-015.md)
- [ADR-016: Golden rendering supplements semantic, property, and conformance tests](ADR-016.md)
- [ADR-017: Percentile performance evidence is reproducible and non-flaky](ADR-017.md)
- [ADR-018: Lopper is the first proving client, not the framework boundary](ADR-018.md)
