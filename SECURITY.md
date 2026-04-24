# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in the **Open Component Model**, please report it
responsibly through one of the channels below. **Do not open a public issue for security
vulnerabilities.**

### GitHub Private Vulnerability Reporting (Preferred)

Please use GitHub's built-in private vulnerability reporting:

1. Navigate to the **Security** tab of this repository.
2. Click **Report a vulnerability**.
3. Fill in the details and submit.

Direct link: [Report a vulnerability](https://github.com/open-component-model/open-component-model/security/advisories/new)

For more information, see [Privately reporting a security vulnerability](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability).

### Email (Fallback)

If you do not have a GitHub account, you may report vulnerabilities via email to the
Technical Steering Committee at
[open-component-model-tsc@lists.neonephos.org](mailto:open-component-model-tsc@lists.neonephos.org).

## Security Contacts

The [Technical Steering Committee (TSC)](docs/steering/OWNERS.md) collectively serves as the
security contact for this project.

| Name | Handle | Role |
|------|--------|------|
| Jakob Moeller | [@jakobmoellerdev](https://github.com/jakobmoellerdev) | TSC Chair / Lead Security Contact |
| TSC Members | See [OWNERS.md](docs/steering/OWNERS.md) | Security Contacts |

## Supported Versions

Only the latest minor release receives security updates. Given the project's pre-1.0 status and
biweekly release cadence, we do not maintain long-term support branches.

| Version | Supported |
|---------|-----------|
| Latest minor (currently 0.x) | Yes |
| Older minors | No |

## Response Process

This project follows the [NeoNephos Security Guidelines](https://github.com/neonephos/guidelines-development/blob/main/security-guidelines/security-guidelines.md)
for vulnerability handling. In summary:

- **Initial response**: We will respond to your report within **14 calendar days**, in line with
  the [OpenSSF Best Practices](https://www.bestpractices.dev/) requirement.
- **Embargo**: Vulnerability details will remain confidential for up to **90 days** while a fix is
  developed. Fix timelines (see table below) are measured from triage completion.
- **Disclosure**: Once a fix is available, we will publish a security advisory with full details.

### Severity Response Targets

| Severity | CVSS Score | Fix Target | Disclosure Target |
|----------|------------|------------|-------------------|
| Critical | 9.0 - 10.0 | ≤ 14 days | ≤ 30 days |
| High | 7.0 - 8.9 | ≤ 30 days | ≤ 60 days |
| Medium | 4.0 - 6.9 | ≤ 90 days | ≤ 90 days |
| Low | 0.1 - 3.9 | Best effort | Best effort |

Timelines are in calendar days, measured from triage completion. For the full definitions, see the
[NeoNephos Security Guidelines, Section 7](https://github.com/neonephos/guidelines-development/blob/main/security-guidelines/security-guidelines.md#7-severity-classification-and-response-targets).

## Disclosure Policy

We follow [coordinated disclosure](https://en.wikipedia.org/wiki/Coordinated_vulnerability_disclosure).
We ask that you:

- Allow us reasonable time to investigate and address the vulnerability before public disclosure.
- Do not exploit the vulnerability beyond what is necessary to demonstrate the issue.
- Do not access or modify data belonging to other users.

We are committed to crediting reporters in our security advisories unless you prefer to remain
anonymous.

## Security Documentation

For details on OCM's security design principles, threat model, and mitigations, refer to:

- Secure Design (in `docs/security/secure-design.md`)
- Security Assurance Case (in `docs/security/assurance-case.md`)

## Past Security Advisories

None yet. See [Published Security Advisories](https://github.com/open-component-model/open-component-model/security/advisories?state=published)
once advisories are available.
