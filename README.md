# ohmesh

`ohmesh` is a lightweight central API platform for static frontend apps deployed
on GitHub Pages or custom domains. It provides OAuth login, cookie-backed
sessions, app/domain registration, and per-user JSON CRUD storage.

## Run

```sh
cp .env.example .env
go run ./cmd/ohmesh
```

Or use Make:

```sh
make start
make health
```

Then open:

```text
http://localhost:8080
```

Useful local commands:

```sh
make run      # foreground server
make start    # background server
make stop
make logs
make check    # gofmt, tests, build
```

## Kubernetes

The repository includes a Dockerfile, GitHub Actions container build workflow,
and Kubernetes manifests.

Image:

```text
ghcr.io/jungju/ohmesh:main
```

Deploy with kubectl:

```sh
make k8s-deploy
make k8s-status
```

For local access:

```sh
make k8s-port-forward
```

Then open:

```text
http://localhost:8080
```

The raw kubectl equivalent is:

```sh
kubectl apply -k deploy/k8s
kubectl -n ohmesh rollout status deploy/ohmesh
```

The service reads configuration from environment variables. For local
development you can export values manually or use a shell that loads `.env`.

Required for GitHub OAuth:

- `GITHUB_CLIENT_ID`
- `GITHUB_CLIENT_SECRET`
- `OHMESH_SESSION_SECRET`

Optional for Google OAuth:

- `GOOGLE_CLIENT_ID`
- `GOOGLE_CLIENT_SECRET`

For production frontends on separate domains, serve ohmesh over HTTPS and set
`OHMESH_COOKIE_SECURE=true` so browser credentials work cross-site.

Register the OAuth callback URL in GitHub as:

```text
http://localhost:8080/auth/github/callback
```

Register the OAuth callback URL in Google as:

```text
http://localhost:8080/auth/google/callback
```

## Core Endpoints

- `GET /`
- `GET /login`
- `GET /admin/apps`
- `GET /admin/apps/:slug`
- `GET /admin/apps/:slug/users`
- `GET /admin/apps/:slug/db`
- `GET /healthz`
- `GET /auth/github/login?app={slug}&redirect_url={url}`
- `GET /auth/github/callback`
- `GET /auth/google/login?app={slug}&redirect_url={url}`
- `GET /auth/google/callback`
- `GET /auth/me`
- `POST /auth/logout`
- `POST /api/apps`
- `GET /api/apps`
- `GET /api/apps/:slug`
- `PATCH /api/apps/:slug`
- `POST /api/apps/:slug/domains`
- `GET /api/apps/:slug/domains`
- `DELETE /api/apps/:slug/domains/:id`
- `GET /api/apps/:slug/records`
- `POST /api/apps/:slug/records`
- `GET /api/apps/:slug/records/:id`
- `PATCH /api/apps/:slug/records/:id`
- `DELETE /api/apps/:slug/records/:id`

## Record Shape

Records are scoped by app and current user session:

```json
{
  "type": "note",
  "data": {
    "title": "Hello",
    "done": false
  }
}
```

`data` is stored as JSON text in SQLite.
