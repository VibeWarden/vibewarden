# Security Policy

Thank you for helping keep VibeWarden and its users safe. We take security
seriously and appreciate responsible disclosure.

---

## Supported Versions

| Version | Supported |
|---------|-----------|
| v0.1.x  | Yes       |

Older releases are not patched once a newer minor version is available.
We encourage all users to stay on the latest release.

---

## Reporting a Vulnerability

### Preferred: GitHub Security Advisories

Open a private advisory directly on GitHub:

[https://github.com/vibewarden/vibewarden/security/advisories/new](https://github.com/vibewarden/vibewarden/security/advisories/new)

This is our preferred channel. It keeps the full conversation in one place, is
end-to-end encrypted, and lets us assign a CVE once a fix is ready.

### Alternative: Email

If you cannot use GitHub, email us at:

**security@vibewarden.dev**

Include as much detail as possible:

- A description of the vulnerability and its potential impact
- Steps to reproduce or a proof-of-concept
- Affected versions (if known)
- Suggested remediation (if any)

**Do not open a public GitHub issue for security vulnerabilities** until a fix
has been released.

---

## Response Timeline

| Milestone | Target |
|-----------|--------|
| Acknowledgement | Within 48 hours of receiving your report |
| Initial assessment | Within 5 business days |
| Fix released | Within 30 days for confirmed vulnerabilities |
| Public disclosure | Within 90 days of the initial report |

We follow coordinated disclosure. If we need more time than the 90-day window
allows, we will communicate that to you before the deadline and agree on an
extension together.

---

## Disclosure Policy

1. Reporter notifies us privately via GitHub Security Advisories or email.
2. We confirm the issue and develop a fix.
3. We release a patch and publish a GitHub Security Advisory (CVE assigned where applicable).
4. Reporter is credited (with permission) in `ACKNOWLEDGMENTS.md` and in the release notes.

We will keep you informed throughout the process and aim to move quickly.

---

## Credit Policy

Researchers who responsibly disclose vulnerabilities deserve public recognition.
With your permission, we will credit you by name or handle in:

- `ACKNOWLEDGMENTS.md`
- The GitHub Security Advisory for the issue
- The release notes for the fixing version

If you prefer to remain anonymous, let us know and we will honour that
preference without question.

VibeWarden does not currently operate a paid bug bounty programme. We offer
public credit and our sincere gratitude.

---

## What Qualifies as a Security Issue

Report the following through our private disclosure process:

- Authentication or authorisation bypass
- Remote code execution or command injection
- SQL injection or other injection attacks
- Path traversal or directory traversal
- Privilege escalation
- Sensitive data exposure (credentials, secrets, PII)
- Cryptographic weaknesses
- Denial of service with low effort or no authentication required
- Security misconfiguration in defaults that silently leaves users exposed

The following are **not** security issues and should be filed as regular GitHub
issues:

- Bugs that do not have a security impact
- Feature requests or UX improvements
- Documentation errors
- Performance regressions (unless exploitable for denial of service)
- Dependency version bumps without a confirmed exploitable path

If you are unsure whether something qualifies, default to private disclosure.
We would rather investigate a non-issue than miss a real vulnerability.

---

## Scope

This policy covers:

- The VibeWarden open-source sidecar binary and all code in this repository
- The VibeWarden CLI (`vibew`)

It does not cover third-party components (Caddy, Ory Kratos, OpenBao, etc.).
Please report vulnerabilities in those projects to their respective maintainers.
