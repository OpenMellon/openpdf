# Security policy

## Supported versions

Security fixes are made on `main` and included in the next release. Only the latest version is supported (the
newest release, or `main` if the problem is not in a release yet), so please check it is still present there before
reporting.

## Reporting a vulnerability

**Do not open a public issue for security problems.**

Report vulnerabilities privately through GitHub:
[Report a vulnerability](https://github.com/OpenMellon/openpdf/security/advisories/new) (Security tab →
**Report a vulnerability**). Only the maintainers can see the report.

Please include:

- A description of the vulnerability and its impact
- The affected package(s) and OpenPDF version or commit
- Steps to reproduce, ideally a minimal Go program and the PDF that triggers it
- Any suggested fix

Many PDF vulnerabilities come from parsing untrusted files (crashes, infinite loops, excessive memory use, or
problems in encryption and signature handling), so a crafted PDF is often the most useful thing you can attach.

## What happens next

The maintainers will acknowledge the report, investigate, and keep you updated in the advisory. Once a fix is
released, the advisory is published with credit to you, unless you would rather stay anonymous.
