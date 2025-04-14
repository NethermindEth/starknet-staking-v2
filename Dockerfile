# ---- Builder Stage ----
FROM golang:1.24-alpine AS builder

WORKDIR /app

RUN apk add --no-cache make

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN make validator


# ---- Final Stage ----
FROM alpine:3.21 AS final

RUN apk add --no-cache ca-certificates

RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

# Copy only the built binary from the builder
COPY --from=builder --chown=appuser:appgroup /app/build/validator /app/validator

# Create config dir
RUN mkdir -p /app/config && chown appuser:appgroup /app/config

# Run as non-root user
USER appuser

ENTRYPOINT ["/app/validator"]