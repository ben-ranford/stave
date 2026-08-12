# Secret Incident

- Owner: Security owner
- Proof command: `go test ./secret ./semantic ./replay ./render/...`

## Trigger

Use this runbook when a secret appears or is suspected to appear in semantic state, snapshots, diagnostics, logs, replay artifacts, or rendered output.

## Immediate actions

1. Stop release or rollout immediately.
2. Identify the affected secret field, artifact type, and retention surface.
3. Revoke or rotate the exposed secret using the owning application policy.

## Diagnose

- Determine whether the leak originated in input capture, handler output, diagnostics, panic recovery, observer export, or replay persistence.
- Confirm whether the data path bypassed opaque handles or recursive redaction.
- Identify every artifact that stored or transmitted the leaked value.

## Recovery requirement

- Remove or invalidate affected artifacts where policy permits.
- Patch the redaction boundary before resuming rollout.
- Add or strengthen an adversarial regression test for the leak path.

## Exit criteria

- Secret exposure scope is fully enumerated.
- Rotation and remediation are complete.
- Redaction proof command is green and the release block is lifted by the named owner.

## Related

- [Security and Threat Model](../security.md)
- [Release, Rollout, and Rollback Policy](../release.md)
