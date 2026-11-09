# Security Policy

## Supported versions

Only the latest release receives security fixes while RUNTIDE is pre-1.0.

## Reporting a vulnerability

Please do **not** open a public issue. Use GitHub's private "Report a vulnerability" feature on the repository
(Security tab > Advisories). *Maintainer: enable private vulnerability reporting and, if you prefer email, add the
address here before publishing.*

Include: affected version, steps to reproduce, impact, and whether the issue needs a local user, another local
process or a malicious workspace file. You should get an acknowledgement within a few days.

## Scope highlights

In scope: the loopback API (auth bypass, origin/host checks), workspace import and path containment, command
execution without approval, secret leakage in logs/UI, unsafe process termination, installer and updater behaviour.

Out of scope: attacks requiring administrator rights already, or code running as the same user reading its own
`api.json` (documented limitation in [docs/SECURITY_MODEL.md](docs/SECURITY_MODEL.md)).
