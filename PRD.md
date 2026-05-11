# ohmesh Product Requirements Document

## 1. Product Summary

`ohmesh` is a lightweight central API platform for static frontend apps. It lets
independent frontend apps share one backend for login, sessions, app
registration, domain registration, and user-scoped JSON data storage.

The product is API-first. Simple server-rendered admin or developer pages may be
added later, but v1 must work without them.

## 2. Problem

Small static apps are easy to deploy but often need the same backend features:

- OAuth login
- Session handling
- Per-user persistence
- CORS-safe domain registration
- A simple CRUD API

Building those features separately for each app creates repeated work and
inconsistent security behavior.

## 3. Goals

- Provide one reusable API backend for many static frontend apps.
- Let each app register its own slug, display name, redirect URL, and domains.
- Let users log in with GitHub or Google OAuth.
- Store each user's app data as flexible JSON records.
- Keep v1 simple enough to run as one Go process with SQLite.

## 4. Non-Goals

- Organization accounts
- Team workspaces
- App memberships
- Public/private sharing between users
- Billing
- Admin dashboards as a hard requirement
- Kubernetes, Redis, message queues, or microservices

## 5. Target Users

### App Developer

A developer who builds small static apps and wants a shared backend for auth and
data.

Needs:

- Register an app.
- Register allowed frontend domains.
- Start OAuth login from a static frontend.
- Use generic JSON CRUD endpoints.

### End User

A person who logs in to one static app using GitHub.

Needs:

- Log in safely.
- Stay signed in through a cookie session.
- Access only their own data.

## 6. Core User Stories

### App Registration

As an app developer, I want to register an app slug and redirect URL so that my
static frontend can use `ohmesh`.

Acceptance criteria:

- App slug is unique.
- App status can be `active` or `disabled`.
- Disabled apps cannot start login or access records.

### Domain Registration

As an app developer, I want to register allowed frontend domains so that OAuth
redirects and CORS are limited to trusted domains.

Acceptance criteria:

- Domains must be valid `http` or `https` URLs.
- An app can have multiple domains.
- One domain can be marked primary.

### OAuth Login

As an end user, I want to log in through GitHub or Google so that I can use an
app without creating a separate password.

Acceptance criteria:

- Login starts with app slug and redirect URL.
- Redirect URL must belong to the app.
- OAuth callback creates or updates the user and identity.
- Session cookie is set after successful login.

### Developer Pages

As an app developer, I want simple server-rendered pages so that I can inspect
apps, users, and app records without writing a separate admin frontend.

Acceptance criteria:

- Intro page explains what ohmesh does.
- Login page exposes GitHub and Google login buttons.
- App management page supports app and domain management.
- App user page shows users with sessions or records for an app.
- App DB page shows app records and supports basic record creation/deletion.

### Current User Session

As a frontend app, I want to fetch the current user so that I can render signed
in state.

Acceptance criteria:

- `GET /auth/me` returns user, app, and session expiration.
- Expired or missing sessions return an unauthorized response.

### JSON Record CRUD

As a frontend app, I want to store arbitrary JSON records per user so that each
app can define its own data shape.

Acceptance criteria:

- Records require a `type`.
- Records require valid JSON `data`.
- List can filter by `type`.
- A user can only access records owned by that user for the current app.

## 7. Functional Requirements

- The API must expose `GET /healthz`.
- The API must support GitHub OAuth login.
- The API must support Google OAuth login.
- The API must persist users, identities, apps, domains, sessions, and records.
- The API must use cookie-based sessions.
- Session tokens must be hashed before database storage.
- `AppRecord.Data` must be stored as JSON text in SQLite.
- CORS must allow configured origins and registered app domains.

## 8. Security Requirements

- OAuth state must be signed.
- Session cookies must be HTTP-only.
- Production cookies must be secure.
- Record APIs must enforce user and app scoping.
- Redirect URLs must be validated against registered app URLs.
- Access and refresh tokens must not be returned in API responses.

## 9. Success Metrics

- A new app can be registered and used by a static frontend.
- A user can complete GitHub OAuth and retrieve `GET /auth/me`.
- A user can create, list, update, and delete JSON records.
- Record access is isolated between users and apps.
- The service can run locally with SQLite using documented environment
  variables.

## 10. Open Questions

- Should app registration require an admin token before first public deployment?
- Should v1 include a minimal server-rendered developer console?
- Should OAuth sessions be one cookie per app or one shared cookie with app-bound
  sessions?
- Should access tokens be encrypted at rest?
