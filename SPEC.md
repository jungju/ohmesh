# ohmesh Technical Specification

## 1. Overview

`ohmesh` is a lightweight central API platform for multiple static frontend apps.
Each frontend app may live in a separate repository and use a separate domain,
while relying on `ohmesh` for authentication, sessions, app registration, domain
registration, and per-user JSON data storage.

Version 1 is intentionally small:

- Single Go API service
- Gin HTTP router
- GORM ORM
- SQLite database
- GitHub OAuth provider
- Google OAuth provider
- Cookie-based sessions
- JSON text storage for flexible app records
- No organization, team, role, or membership model

## 2. Architecture

### Runtime

`ohmesh` runs as one HTTP API server.

Entry point:

- `cmd/ohmesh/main.go`

Core packages:

- `internal/config`: environment configuration
- `internal/models`: database models and migration
- `internal/server`: HTTP router, middleware, auth, app, domain, and record APIs

### Data Store

SQLite is the v1 database. GORM auto-migration creates and updates the schema at
startup.

`AppRecord.Data` is stored as SQLite `TEXT` and validated as JSON before write.

## 3. Domain Model

### User

A person who logs in through OAuth.

Fields:

- `ID`
- `Email`
- `Name`
- `AvatarURL`
- `CreatedAt`
- `UpdatedAt`

### Identity

An OAuth identity connected to a user.

Fields:

- `ID`
- `UserID`
- `Provider`
- `ProviderUserID`
- `AccessToken`
- `RefreshToken`
- `CreatedAt`
- `UpdatedAt`

Constraints:

- `Provider + ProviderUserID` must be unique.

### App

A static frontend app using `ohmesh` as its backend.

Fields:

- `ID`
- `Slug`
- `Name`
- `DefaultRedirectURL`
- `Status`
- `CreatedAt`
- `UpdatedAt`

Status values:

- `active`
- `disabled`

Constraints:

- `Slug` must be unique.
- `Slug` uses lowercase letters, numbers, and hyphens.

### AppDomain

An allowed frontend origin or base URL for an app.

Fields:

- `ID`
- `AppID`
- `Domain`
- `IsPrimary`
- `CreatedAt`
- `UpdatedAt`

Constraints:

- `AppID + Domain` must be unique.
- Domains must use `http` or `https`.

### Session

A login session for one user and one app.

Fields:

- `ID`
- `UserID`
- `AppID`
- `TokenHash`
- `ExpiresAt`
- `CreatedAt`
- `UpdatedAt`

Security:

- Raw session tokens are stored only in cookies.
- The database stores only `TokenHash`.
- Expired sessions are rejected.

### AppRecord

Generic per-user data for an app.

Fields:

- `ID`
- `AppID`
- `UserID`
- `Type`
- `Data`
- `CreatedAt`
- `UpdatedAt`

Rules:

- Records are always scoped to the current session user and app.
- Users cannot read or mutate another user's records.
- `Data` must be valid JSON.

## 4. HTTP API

### Health

```http
GET /healthz
```

Response:

```json
{
  "ok": true
}
```

### GitHub OAuth

```http
GET /auth/github/login?app={slug}&redirect_url={url}
GET /auth/github/callback
GET /auth/google/login?app={slug}&redirect_url={url}
GET /auth/google/callback
GET /auth/me
POST /auth/logout
```

Login behavior:

- The app slug must point to an active app.
- The redirect URL must match the app default redirect URL or a registered app
  domain.
- OAuth state is signed using `OHMESH_SESSION_SECRET`.
- On callback, `ohmesh` upserts the user and identity, creates a session, sets a
  cookie, and redirects back to the frontend.

### Server-Rendered Pages

```http
GET /
GET /login
GET /admin/apps
GET /admin/apps/:slug
GET /admin/apps/:slug/users
GET /admin/apps/:slug/db
```

These pages are developer/admin convenience views built with Go templates. They
are not a replacement for the API.

### Apps

```http
POST /api/apps
GET /api/apps
GET /api/apps/:slug
PATCH /api/apps/:slug
```

Create request:

```json
{
  "slug": "notes",
  "name": "Notes",
  "default_redirect_url": "https://example.com/notes",
  "status": "active"
}
```

### App Domains

```http
POST /api/apps/:slug/domains
GET /api/apps/:slug/domains
DELETE /api/apps/:slug/domains/:id
```

Create request:

```json
{
  "domain": "https://username.github.io/notes",
  "is_primary": true
}
```

### App Records

```http
GET /api/apps/:slug/records
POST /api/apps/:slug/records
GET /api/apps/:slug/records/:id
PATCH /api/apps/:slug/records/:id
DELETE /api/apps/:slug/records/:id
```

Create request:

```json
{
  "type": "note",
  "data": {
    "title": "Hello",
    "done": false
  }
}
```

List query parameters:

- `type`: optional record type filter
- `limit`: optional, default `100`, maximum `500`
- `offset`: optional, default `0`

## 5. Configuration

Environment variables:

- `OHMESH_ADDR`
- `OHMESH_DATABASE_PATH`
- `OHMESH_ENV`
- `OHMESH_SESSION_SECRET`
- `OHMESH_SESSION_COOKIE`
- `OHMESH_SESSION_TTL`
- `OHMESH_COOKIE_SECURE`
- `OHMESH_ALLOWED_ORIGINS`
- `GITHUB_CLIENT_ID`
- `GITHUB_CLIENT_SECRET`
- `GOOGLE_CLIENT_ID`
- `GOOGLE_CLIENT_SECRET`

Production requirements:

- Use HTTPS.
- Set `OHMESH_COOKIE_SECURE=true`.
- Use a long random `OHMESH_SESSION_SECRET`.
- Restrict app registration endpoints before public exposure.

## 6. Non-Goals For v1

- Organizations
- Teams
- Memberships
- Role-based permissions
- Redis
- Kubernetes
- Message queues
- Microservices
- OAuth providers beyond GitHub and Google
- Schema-specific app data tables
