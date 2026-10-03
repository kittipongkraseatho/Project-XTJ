# Multi-stage build for the Go API.
# Stage 1: build
# Stage 2: distroless runtime

FROM golang:1.23-alpine AS build
WORKDIR /src

# Cache deps
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Run sqlc generate if queries/ changed; sqlc.yaml must be present
RUN go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest \
 && sqlc generate

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/api ./cmd/api

# ── Stage 2: runtime (distroless) ─────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/api /app/api
COPY --from=build /src/migrations /app/migrations
ENV MIGRATIONS_DIR=/app/migrations
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/app/api"]