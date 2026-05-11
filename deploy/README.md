# Kubernetes Deployment

This directory contains a minimal Kubernetes deployment for `ohmesh`.

The default image is:

```text
ghcr.io/jungju/ohmesh:main
```

The GitHub Actions workflow builds the image for `linux/arm64`, matching the
local Kubernetes cluster.

Deploy:

```sh
kubectl apply -k deploy/k8s
kubectl -n ohmesh rollout status deploy/ohmesh
```

Or use Make:

```sh
make k8s-deploy
make k8s-status
```

Local access options:

```sh
make k8s-port-forward
```

Then open:

```text
http://localhost:8080
```

If your local ingress controller supports `*.localhost`, the included ingress is
available at:

```text
http://ohmesh.localhost
```

OAuth provider credentials are optional for the pod to start. To enable real
GitHub or Google login, export the credential environment variables and run:

```sh
make k8s-oauth-secret
kubectl -n ohmesh rollout restart deploy/ohmesh
```

If your GHCR package is private, create the pull secret used by the deployment:

```sh
make k8s-ghcr-secret
```

The default SQLite database is stored on a `PersistentVolumeClaim` named
`ohmesh-data`.
