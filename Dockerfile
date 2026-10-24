# syntax=docker/dockerfile:1
FROM node:22-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
COPY db ./db
RUN mkdir -p ./internal/webui/dist
COPY --from=web /src/web/dist/ ./internal/webui/dist/
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
  -o /out/requestarr ./cmd/requestarr

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/requestarr /requestarr
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/requestarr"]
