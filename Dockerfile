# syntax=docker/dockerfile:1
# The Odraz gateway: one static binary with the reference web UI, run as a non-root user with no shell.
FROM golang:1.23 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/odraz-gateway ./cmd/odraz-gateway \
 && mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/odraz-gateway /odraz-gateway
# /data holds the encrypted session file (-session-file /data/sessions.bin); mount a volume here
COPY --from=build --chown=65532:65532 /out/data /data
ENV ODRAZ_LISTEN=0.0.0.0:8080
EXPOSE 8080
VOLUME /data
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s CMD ["/odraz-gateway", "-healthcheck"]
ENTRYPOINT ["/odraz-gateway"]
