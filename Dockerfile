# ---- Builder Stage ----
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

WORKDIR /app

RUN apk add --no-cache make git

COPY go.mod go.sum ./

RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION

RUN CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" make validator VERSION="$VERSION"


# ---- Final Stage ----
FROM alpine:3.24 AS final

RUN apk upgrade --no-cache && apk add --no-cache ca-certificates tzdata

RUN addgroup -S -g 65532 nonroot && adduser -S -D -u 65532 -G nonroot -h /home/nonroot nonroot

WORKDIR /app

# Copy only the built binary from the builder
COPY --from=builder /app/build/validator /app/validator

# Create config dir
RUN mkdir -p /app/config && chown 65532:65532 /app/config

# Run as non-root user
USER 65532:65532

ENTRYPOINT ["/app/validator"]
