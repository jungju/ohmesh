# ohmesh Task List

## Completed

- [x] Initialize Go module.
- [x] Add Gin HTTP server.
- [x] Add GORM SQLite connection.
- [x] Add environment-based configuration.
- [x] Add database models for users, identities, apps, domains, sessions, and
      records.
- [x] Add GORM auto-migration.
- [x] Add `GET /healthz`.
- [x] Add GitHub OAuth login and callback flow.
- [x] Add Google OAuth login and callback flow.
- [x] Add cookie-based session creation and lookup.
- [x] Hash session tokens before database storage.
- [x] Add app CRUD endpoints.
- [x] Add app domain CRUD endpoints.
- [x] Add per-user app record CRUD endpoints.
- [x] Add JSON validation for record data.
- [x] Add basic smoke tests.
- [x] Add README and environment example.
- [x] Add Go template intro page.
- [x] Add Go template login page with GitHub and Google buttons.
- [x] Add Go template app management pages.
- [x] Add app user and app DB management pages.
- [x] Add Dockerfile for container packaging.
- [x] Add GitHub Actions workflow to publish GHCR image.
- [x] Add Kubernetes manifests for kubectl deployment.
- [x] Add Makefile targets for local and Kubernetes operations.

## Next

- [ ] Protect app and domain registration endpoints with an admin secret or
      local-only bootstrap mode.
- [ ] Protect server-rendered management pages with admin authentication.
- [ ] Add request/response API examples for each endpoint.
- [ ] Add integration tests for OAuth state signing and redirect validation.
- [ ] Add tests proving one user cannot read another user's records.
- [ ] Add tests proving one app cannot read another app's records.
- [ ] Add expired session cleanup.
- [ ] Add structured logging.
- [ ] Add graceful shutdown.
- [ ] Add database backup guidance for SQLite.
- [ ] Add production deployment notes.
- [ ] Replace local development Kubernetes config values with production
      secrets and TLS settings.

## Later

- [ ] Add optional server-rendered developer pages.
- [ ] Add app creation and domain management UI.
- [ ] Add additional OAuth providers.
- [ ] Add encrypted token storage.
- [ ] Add rate limiting.
- [ ] Add audit events for login and app configuration changes.
- [ ] Add import/export endpoint for app records.
- [ ] Add pagination metadata to record list responses.

## Release Checklist

- [ ] Run `gofmt`.
- [ ] Run `go test ./...`.
- [ ] Run `go build ./cmd/ohmesh`.
- [ ] Review `.env.example`.
- [ ] Review README setup instructions.
- [ ] Confirm app registration is protected before public deployment.
