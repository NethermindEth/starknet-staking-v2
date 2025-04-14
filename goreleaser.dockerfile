FROM alpine:3.21

RUN apk add --no-cache ca-certificates

RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

RUN mkdir -p /app/config && chown appuser:appgroup /app/config

COPY --chown=appuser:appgroup validator /app/validator

USER appuser

ENTRYPOINT ["/app/validator"]