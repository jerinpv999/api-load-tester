# ---- build ----
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/loadforge .

# ---- runtime ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata wget \
    && adduser -D -u 10001 loadforge \
    && mkdir -p /data && chown loadforge /data
COPY --from=build /out/loadforge /usr/local/bin/loadforge
USER loadforge
ENV PORT=8080 DATA_DIR=/data
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=15s --timeout=3s --retries=3 CMD wget -qO- http://127.0.0.1:8080/api/health || exit 1
ENTRYPOINT ["/usr/local/bin/loadforge"]
