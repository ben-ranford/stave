# Stave v1 Architecture and Governance

These documents translate the approved Stave v1 proposal, PRD, and system design into tracked repository policy. They define the production contract and distinguish implemented package evidence from remaining release blockers. The branch is not GA-ready until the release gates below are green.

## Scope

- Authoritative for tracked architecture and governance policy under `docs/`.
- Implementation-facing only: package boundaries, compatibility rules, operational gates, and incident response.
- Source inputs remain the offline proposal pack and Ralph context; this tree does not restate them verbatim.

## Current repo status

- The checked-in implementation proves the package split, semantic nodes, capability negotiation (including full-colour degradation), deterministic snapshots/replay, typed actions, bounded runtime paths, secret handling, layout/cells, protocol transport, P0 primitives, and the synthetic/component performance report. Live SSH loopback evidence remains separate in the SSH adapter test.
- Remaining release blockers are the root facade/API freeze, external Lopper application migration and rollback evidence, platform terminal soak, and completion of P1 breadth.
- When a document says `Planned`, it is a required contract that is not yet fully evidenced by this branch; it is not a claim that the package is absent.

## Reading order

1. [Architecture Overview](architecture.md)
2. [Compatibility, Versioning, and Schema Policy](compatibility.md)
3. [Security and Threat Model](security.md)
4. [Accessibility and Agent Action Parity](accessibility-agent-parity.md)
5. [Dependency Policy](dependencies.md)
6. [Release, Rollout, and Rollback Policy](release.md)
7. [Performance Budgets](performance.md)
8. [Primitive Contract Checklist](primitives.md)
9. [Client Adoption Guide](client-adoption.md)
10. [Atlas Regression and Proof Rig](atlas-regression-rig.md)
11. [Lopper Migration and Rollback Plan](lopper-migration.md)
12. [ADRs](adr/README.md)
13. [Operational Runbooks](runbooks/README.md)

## Owner and proof convention

Every operational policy and runbook must carry:

- `Owner`: a role placeholder that must be replaced before pilot or GA.
- `Proof command`: a canonical local or CI command placeholder that must exist before the control is considered complete.

Until the repo has a canonical verification surface, placeholders remain intentionally explicit rather than inferred.
