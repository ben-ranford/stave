# Security and Threat Model

## Scope

The Stave security boundary protects typed control-plane operations from untrusted terminal and agent-facing content. The primary invariants are prompt-injection resistance, terminal-sink safety, secret exclusion, bounded resources, and fail-closed action handling.

## Trust model

Untrusted inputs:

- Rendered application text
- Files, reports, logs, remote data, and tool output
- Agent-provided action arguments
- Theme and config files before validation
- Terminal environment capability hints
- Adapter framework events and paste content

Trusted only after validation:

- Registered action definitions
- Application policy
- Valid confirmation grants
- Renderer-generated control sequences
- Versioned schemas compiled into the application

## Threats and controls

| Threat | Required control | Runbook |
|---|---|---|
| Prompt-injection text attempts to steer actions | Render content as data only; action selection comes only from the registry | [Protocol or Handler Timeout](runbooks/protocol-handler-timeout.md) |
| Content includes ANSI or terminal control bytes | Strip or reject control sequences at content boundaries; only renderer emits terminal control | [Broken Terminal and Restoration](runbooks/broken-terminal-restoration.md) |
| Secret data leaks into snapshot, log, or replay | Opaque handles, structured redaction, and no plaintext in semantic state | [Secret Incident](runbooks/secret-incident.md) |
| Agent uses stale or replaced targets | Require `nodeId`, `generation`, and `observedRevision`; fail with structured errors | [Replay Divergence](runbooks/replay-divergence.md) |
| Capability hints lie or drift | Negotiate immutable manifests and downgrade explicitly | [Adapter Incompatibility](runbooks/adapter-incompatibility.md) |
| Theme/config file changes degrade safety or clarity | Validate token completeness and fallback policy before session start | [Theme Validation](runbooks/theme-validation.md) |
| Resource exhaustion | Bound node counts, text size, queue depth, protocol line size, render size, and concurrent effects | [Protocol or Handler Timeout](runbooks/protocol-handler-timeout.md) |

## Required policies

### Prompt-injection boundary

- Semantic text fields are data only.
- Content cannot declare actions, schemas, confirmations, capabilities, or policy.
- Consequential actions require policy approval and optional confirmation grants.
- Coordinate or raw-terminal fallback is off by default and must be explicitly negotiated.
- Action schemas must reject executable expressions and unsupported fields.

### Terminal injection boundary

- Reject ESC, CSI, OSC, DCS, and device-control sequences in application content.
- Sanitize diagnostics before they reach a terminal sink.
- Disable clipboard OSC by default.
- Machine stdout may carry protocol/output only, never human-targeted diagnostics.

### Secret handling

- Secret fields use opaque handles instead of plaintext values.
- Snapshots, results, diagnostics, panic reports, and replay artifacts must redact secret values recursively.
- Non-interactive mode must fail with a typed capability error when secure input is required and no secure provider exists.

### Confirmation

Confirmation grants must bind to:

- Session
- Action ID and version
- Target ID and generation
- Canonical argument hash
- Observed revision range
- Safety class
- Expiry

Grants are single-use by default and are invalidated by any target, argument, expiry, or policy change.

## Security control table

| Control | Owner | Proof command |
|---|---|---|
| Prompt-injection separation is covered by adversarial tests | Security owner | `go test ./runtime/agent ./protocol ./semantic` |
| Terminal content sanitization is enforced at the final sink | Render owner | `go test ./render/... ./surface` |
| Secret redaction blocks release on leakage | Secret owner | `go test ./secret ./semantic ./replay` |
| Resource limits fail closed without partial action execution | Runtime owner | `go test ./runtime/... ./effect ./session` |

## Related documents

- [Accessibility and Agent Action Parity](accessibility-agent-parity.md)
- [Release, Rollout, and Rollback Policy](release.md)
- [Secret Incident Runbook](runbooks/secret-incident.md)
- [ADR-012](adr/ADR-012.md)
- [ADR-013](adr/ADR-013.md)
