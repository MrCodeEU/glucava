# Contributing

Thanks for looking at Glucava. It is a small, security-sensitive project (glucose data, Strava session cookies, Dexcom credentials), so contributions are welcome but reviewed carefully.

## Before you start

- **Bugs and ideas:** open an issue first for anything larger than a small fix, so we agree on the direction before you spend time.
- **Security problems:** do not open an issue. Use the private report described in [SECURITY.md](SECURITY.md).
- **Never paste secrets or health data** into issues, pull requests or logs: no cookies, tokens, passwords, `.env` contents, glucose exports or screenshots of your real data. Use demo mode (`make mock`) to reproduce things.
- Everything is licensed under the [AGPL-3.0](LICENSE). By contributing you agree that your contribution is released under the same license.

## Development

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for setup, layout and the request flow, and [docs/TESTING.md](docs/TESTING.md) for the manual checklist against real accounts.

```sh
make hooks   # gofmt, vet, lint on commit; race tests and govulncheck on push
make check   # everything CI runs
make mock    # demo server with fake data, no real accounts involved
```

Tests that launch Chrome need `CHROME_PATH`; without it they are skipped.

## Pull requests

- Branch from `main`; `main` is protected and needs the CI checks (`check`, `test`, `lint`, `vuln`) to pass. Squash merge.
- Keep a PR to one topic. Explain why, not only what.
- Add or update tests for behaviour changes. A bug fix should come with a test that failed before.
- Update the docs that describe the behaviour (README, `.env.example`, `docs/`), including the setting names if you add a setting. Every setting must be changeable from the web UI **and** scriptable (`glucava config`) or seedable from the environment.
- Do not hardcode values that a user might want to change (URLs, intervals, selectors, limits). Put them in settings or in `cmd/glucava/tuning.go`.
- Commit messages follow Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`, `chore:`).

## Rules that protect users' data

These are checked in review and are not negotiable:

- Secrets are never logged, put in URLs, returned by a page, or passed in argv. They go through the encrypted vault.
- A change to how the Strava description is written must keep the pre-write check (`render.PreservesText`): the user's own text must survive byte for byte.
- Nothing in tests or docs may touch a real Strava, Dexcom or SMTP account.
- New network calls that use a user-supplied URL must not follow redirects unless there is a reason.

## Adding a glucose source or importer

See "Adding a glucose source" in the README. A live source is one interface plus one registry entry; a file importer is one function registered in `init()`. Please include a small synthetic sample file in a test, never a real export.
