# Security Policy

## Supported surfaces

Security fixes are applied to `main` and the latest released `v*` tag when a maintained release exists. Older tags and feature branches may not receive fixes.

## Reporting

Do not open public issues for suspected vulnerabilities. Report them privately to the repository owner with:

- a concise summary and impact assessment
- affected tags or commit SHAs
- a minimal reproduction or proof
- any suggested mitigation or patch direction

Stave treats node content as inert data, validates names, revisions, and typed action fields before handlers run, and keeps reproducible traces for review. Vulnerability reports should still assume consumers may compose the library into higher-risk applications.

## Response expectations

- Initial triage target: 3 business days
- Status update target: 7 business days
- Fix coordination: best effort based on severity and release impact

Coordinated disclosure is preferred until a fix or mitigation is available.
