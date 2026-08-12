# Theme Validation Failure

- Owner: Theme owner
- Proof command: `go test ./theme ./render/... ./capability`

## Trigger

Use this runbook when a theme or token pack is missing required roles, violates capability fallbacks, or changes semantic meaning unexpectedly.

## Immediate actions

1. Block the affected session or release candidate from promotion.
2. Record the theme name, token-pack version, capability profile, and missing or invalid role set.
3. Confirm whether the failure is completeness, contrast, fallback, or meaning drift.

## Diagnose

- Check required role groups: surface, ink, border, action, status, domain, chart, typography, space, radius, motion, elevation, terminal, and asset.
- Verify dark or light, dense or comfortable, truecolor or ANSI or no-colour, Unicode or ASCII, and reduced-motion variants.
- Confirm whether a token rename is actually a breaking semantic change.

## Recovery requirement

- Missing required roles must fail closed or use an explicitly declared fallback theme.
- Do not silently substitute raw colour literals in primitives.
- Publish migration notes for semantic meaning changes.

## Exit criteria

- Theme validation passes for required capability profiles.
- Any breaking meaning change is versioned and documented.
- Theme proof command is green.

## Related

- [Compatibility, Versioning, and Schema Policy](../compatibility.md)
- [Security and Threat Model](../security.md)
