# Governance

token-advisor is an open source project run by Infrapal. This explains who does
what, how decisions are made, and how the project keeps going if someone leaves.

## Roles

| Role | Who | What they do |
| --- | --- | --- |
| **Users** | Anyone who runs token-advisor | Report bugs, suggest features, and answer each other's questions in [Discussions](https://github.com/infrapalhq/infrapal-token-advisor/discussions) |
| **Contributors** | Anyone who opens a pull request, issue or review | Propose changes, following [CONTRIBUTING.md](CONTRIBUTING.md) and the [Code of Conduct](CODE_OF_CONDUCT.md) |
| **Maintainers** | The [`@infrapalhq/token-advisor-maintainers`](https://github.com/orgs/infrapalhq/teams/token-advisor-maintainers) team | Review and merge pull requests, triage issues, cut releases, handle security reports, and enforce the Code of Conduct |
| **Infrapal** | The company that owns the project | Sets direction, holds the copyright, and makes the final call when maintainers can't agree |

Maintainers are listed by team rather than by name, so the list stays current as
people join or leave. The team's members are shown on its GitHub page.

### What maintainers must do

- Review pull requests, and merge only those that pass CI and have been reviewed
  by someone other than their author. A maintainer may merge without a review only
  for an urgent fix, and says why on the pull request.
- Respond to new issues and pull requests, aiming for a first reply within a week.
- Acknowledge a security report within 3 business days, as
  [SECURITY.md](SECURITY.md) describes, and keep it private until it's fixed.
- Make releases from a tagged commit on `main`, through the release workflow,
  never from a personal machine.
- Act on Code of Conduct reports, as the Code of Conduct describes.

## How decisions are made

Most decisions happen on a pull request or issue, in public. Anyone can comment,
and maintainers aim for agreement.

- **Small changes**, such as bug fixes, docs and tests: one maintainer's approval
  is enough.
- **Bigger changes**, such as a new agent, a new detector, a change to the
  database schema or to what leaves your machine: open an issue first, so the
  approach is agreed before the work is done.
- **When maintainers can't agree**, Infrapal makes the final call, and the reason
  is written on the issue or pull request.
- **Security reports** are handled in private by maintainers, through
  [GitHub Security Advisories](https://github.com/infrapalhq/infrapal-token-advisor/security/advisories).

The roadmap records what's planned.

## Becoming a maintainer

Contributors who have made several substantial contributions, reviewed others'
work, and followed the Code of Conduct can be invited to join the maintainers team.
Any maintainer can propose it, and the other maintainers agree. A maintainer who
steps down, or is inactive for six months, may be moved to an emeritus role and
lose write access, which they can get back by asking.

## Keeping the project going

The project mustn't depend on any single person.

- At least two people have admin access to the repository at all times.
- Releases are built in CI, never on a personal machine, and will be signed there
  with keyless signing, so there's no personal key to lose. Secrets such as the
  Homebrew token are stored as repository secrets that any admin can rotate.
- If a maintainer becomes unavailable, the remaining maintainers continue with
  their access. If no maintainer can act, Infrapal appoints new ones within 30 days.
- The code is MIT licensed, so anyone may fork it if the project ever stops.

## Changing this document

Changes to governance are made by pull request, approved by a maintainer and
agreed by Infrapal.
