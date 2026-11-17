# Security

## Reporting

Open a **private security advisory** on GitHub (Security → Report a vulnerability). Do not open a
public issue for a vulnerability. This is a single-person project, not a company: expect an answer
within a few days, not within an hour.

## What this software assumes

- **It is not multi-tenant.** One owner, no accounts, no roles. Isolation is the proxy in front of it.
- **The proxy is the door.** The binary trusts whoever can reach it; run it on a private network or
  behind an authenticating proxy and never publish the port.
- **Secrets are `0600` files** beside the data, never in git, never in an environment variable that
  ends up in a stack file.
- **Anything the app serves is served on purpose.** Files render only in narrow, listed formats; an
  unknown type is an attachment.
