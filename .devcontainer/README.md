# Development Services

`compose.dev.yaml` provides PostgreSQL, Keycloak, and Apprise API. The imported development realm includes a disposable user `requestarr-admin` / `requestarr-admin` in the `admin` group. These credentials are development-only and must never be used in production.

The Dev Container merges `compose.dev.yaml` first and `.devcontainer/docker-compose.devcontainer.yaml` second. Docker Compose resolves relative build and bind-mount paths from the first Compose file's project directory, so paths in the workspace override are repository-root-relative. The same merged model can be checked from the repository root with:

```bash
docker compose -f compose.dev.yaml -f .devcontainer/docker-compose.devcontainer.yaml config
```

Inside the Dev Container, PostgreSQL is available at `postgres:5432` through `REQUESTARR_DATABASE_URL`. From the host, use `127.0.0.1:5432`; the Compose service name is only resolvable on the shared Compose network.
