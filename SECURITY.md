# Security Policy

Thank you for helping keep VibeWarden and its users safe. We take security
seriously and appreciate responsible disclosure.

## Reporting a Vulnerability

### Critical vulnerabilities

For issues that could lead to remote code execution, privilege escalation,
authentication bypass, or exposure of sensitive data, please report **privately**
by email:

**security@vibewarden.dev**

Include as much detail as possible:

- A description of the vulnerability and its potential impact
- Steps to reproduce or a proof-of-concept
- Any affected versions you have identified
- Suggested remediation if you have one

Do not open a public GitHub issue for critical vulnerabilities until a fix has
been released.

### Non-critical vulnerabilities

For lower-severity issues (hardening improvements, minor information leaks,
dependency advisories), you may open a public GitHub issue or use
[GitHub's private vulnerability reporting](https://github.com/vibewarden/vibewarden/security/advisories/new).

If you are unsure whether an issue is critical, default to the private email.

## Response Time

We aim to:

- **Acknowledge** your report within **48 hours**
- Provide an initial assessment within 5 business days
- Release a fix and publish a security advisory as quickly as the severity warrants

We will keep you informed throughout the process.

## Credit

We believe researchers who responsibly disclose vulnerabilities deserve public
recognition. With your permission, we will credit you by name (or handle) in
`ACKNOWLEDGMENTS.md` and in the release notes for the fix.

If you prefer to remain anonymous, just let us know and we will honour that
preference.

## Bug Bounty

VibeWarden does not currently operate a paid bug bounty programme. We offer
public credit and our sincere gratitude.

## Scope

This policy covers the VibeWarden open-source sidecar binary and the code in
this repository. It does not cover third-party dependencies (Caddy, Ory Kratos,
etc.) — please report those vulnerabilities to their respective projects.

## Disclosure Policy

We follow a **coordinated disclosure** model:

1. Reporter notifies us privately.
2. We confirm the issue and develop a fix.
3. Fix is released and a security advisory is published.
4. Reporter is credited (with permission).

We ask that you give us a reasonable amount of time to address the issue before
any public disclosure.
