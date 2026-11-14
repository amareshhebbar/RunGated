# Security

Rungated is a security product, so we take reports seriously.

## Reporting a vulnerability

Email hebbar.gvamaresh@gmail.com with the subject "Rungated security". Do not open a public issue.

Include the version (`rungated version`), what you did, what happened and what you expected. A proof of concept helps.

You will get a reply within three working days. We will agree on a disclosure date with you and credit you in the changelog unless you ask us not to.

## Scope

In scope: the gateway, the proxies, the Console, the SDKs and the release artifacts in this repository.

Bypasses of a detector are welcome as reports, but they are expected: Rungated detects known patterns and enforces the rules you declare. It does not claim to stop every prompt injection. See `docs/security/limits.md`.
