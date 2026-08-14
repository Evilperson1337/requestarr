FROM mcr.microsoft.com/devcontainers/go:1-1.26-bookworm

RUN apt-get update \
    && apt-get install -y --no-install-recommends postgresql-client jq make curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Node is installed with fnm to keep the image straightforward and reproducible.
RUN curl -fsSL https://fnm.vercel.app/install | bash -s -- --skip-shell \
    && /root/.local/share/fnm/fnm install 22 \
    && /root/.local/share/fnm/fnm default 22
ENV PATH="/root/.local/share/fnm/aliases/default/bin:/root/.local/share/fnm:${PATH}"

RUN go install github.com/go-delve/delve/cmd/dlv@latest \
    && go install github.com/goreleaser/goreleaser/v2@latest
