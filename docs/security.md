# Security contract

## Scope

The Stave security boundary protects typed control-plane operations from
untrusted terminal and agent-facing content. Its primary invariants are
prompt-injection resistance, terminal-sink safety, secret exclusion, bounded
resources, and fail-closed action handling.

## Trust model

Treat rendered application text, files, reports, logs, remote data, tool
output, agent-provided action arguments, theme/config files, terminal
capability hints, framework events, and paste content as untrusted until
validated.

Registered action definitions, application policy, valid confirmation grants,
renderer-generated control sequences, and versioned application schemas are
trusted only after validation.

## Threats and controls

| Threat | Required control |
|---|---|
| Prompt-injection text attempts to steer actions | Render content as data only; action selection comes only from the registry |
| Content includes ANSI or terminal control bytes | Strip or reject control sequences at content boundaries; only renderer emits terminal control |
| Secret data leaks into snapshot, log, or replay | Use opaque handles, structured redaction, and no plaintext in semantic state |
| Agent uses stale or replaced targets | Require `nodeId`, `generation`, and `observedRevision`; fail with structured errors |
| Capability hints lie or drift | Negotiate immutable manifests and downgrade explicitly |
| Theme/config file changes degrade safety or clarity | Validate token completeness and fallback policy before session start |
| Resource exhaustion | Bound node counts, text size, queue depth, protocol line size, render size, and concurrent effects |

## Required policies

### Prompt-injection boundary

- Semantic text fields are data only.
- Content cannot declare actions, schemas, confirmations, capabilities, or
  policy.
- Consequential actions require policy approval and optional confirmation
  grants.
- Coordinate or raw-terminal fallback is off by default and must be explicitly
  negotiated.
- Action schemas reject executable expressions and unsupported fields.

### Terminal injection boundary

- Reject ESC, CSI, OSC, DCS, and device-control sequences in application
  content.
- Sanitize diagnostics before they reach a terminal sink.
- Disable clipboard OSC by default.
- Keep machine stdout free of human-targeted diagnostics.

### Secret handling

- Use opaque handles instead of plaintext secret values.
- Redact secret values recursively from snapshots, results, diagnostics, panic
  reports, and replay artifacts.
- In non-interactive mode, return a typed capability error when secure input is
  required and no secure provider exists.

### Confirmation

Confirmation grants bind to session, action ID and version, target ID and
generation, canonical argument hash, observed revision range, safety class,
and expiry. Grants are single-use by default and become invalid after any
target, argument, expiry, or policy change.

## Related guides

- [Accessibility and agent-control expectations](accessibility-agent-parity.md)
- [Compatibility and versioning](compatibility.md)
