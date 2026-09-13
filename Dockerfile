# syntax=docker/dockerfile:1
FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
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
