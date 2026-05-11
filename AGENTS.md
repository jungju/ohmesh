# AGENTS.md

## Project

`ohmesh` is a Go API service for multiple static frontend apps. It provides
GitHub OAuth login, cookie sessions, app/domain registration, and per-user JSON
CRUD storage using Gin, GORM, and SQLite.

## Working Rules

- Keep v1 simple: one Go service, one SQLite database.
- Keep Kubernetes support as optional deployment packaging only; do not split
  the app into microservices.
- Do not add Redis, queues, or microservices.
- Do not add organization, team, role, or membership concepts unless explicitly
  requested.
- Prefer small, focused changes that match the existing package layout.
- Keep API behavior user-scoped by default.
- Never return OAuth access tokens, refresh tokens, or raw session tokens in API
  responses.
- Store flexible app data as validated JSON text.

## Code Style

- Use idiomatic Go.
- Run `gofmt` on changed Go files.
- Keep handlers readable and avoid unnecessary abstractions.
- Add tests for behavior that affects authentication, authorization, session
  handling, redirect validation, or record scoping.
- Keep comments short and useful.

## Development Completion Rule

After finishing any development change:

1. Run `gofmt` on changed Go files.
2. Run `go test ./...`.
3. Run `go build ./cmd/ohmesh`.
4. Commit the completed change with a concise message.
5. Push the current branch to GitHub.

If a frontend package is added later and contains npm scripts, also run:

1. `npm run lint`
2. `npm run build`

The same verification can be run with:

```sh
make check
```

For Kubernetes packaging changes, also run:

```sh
kubectl apply --dry-run=client -k deploy/k8s
```

## Current Caveat

The current repository may not always be initialized as a Git repository in local
development environments. If `git` is unavailable, complete formatting, tests,
and build verification, then report that commit and push could not be performed.
