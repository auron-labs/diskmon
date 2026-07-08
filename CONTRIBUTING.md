# Contributing

Thanks for helping improve diskmon.

## Local Setup

Install the pinned project tools and JavaScript dependencies:

```bash
mise install
bun install --cwd webui --frozen-lockfile
```

Install repository hooks if you want local pre-commit and conventional commit checks:

```bash
hk install --mise
```

## Development Checks

Run the same checks used by CI:

```bash
mise run check
```

Useful focused commands:

```bash
task fmt-check
task vet
task lint-webui
task test-webui
task test
```

Build a local no-CGO Linux binary when you only need a quick compile check:

```bash
task build-linux-amd64-nocgo
```

## Commit Messages

Use conventional commits so release-please can build accurate changelogs and version bumps:

```text
feat: add SMART history endpoint
fix: stop SPA redirect loop
docs: clarify systemd setup
```

Use `feat!:` or a `BREAKING CHANGE:` footer for breaking changes.

## Pull Requests

Before opening a PR, include:

1. A concise summary of behavior changes.
2. Affected modules or paths.
3. Test, lint, and build commands run.
4. Screenshots for UI changes when relevant.

## Security

Do not report security vulnerabilities in public issues. Follow `SECURITY.md` instead.
