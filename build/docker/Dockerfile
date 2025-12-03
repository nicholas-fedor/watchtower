ARG BASE_IMAGE=alpine:3.23.0@sha256:51183f2cfa6320055da30872f211093f9ff1d3cf06f39a0bdb212314c5dc7375
ARG GOOS=linux
ARG GOARCH

FROM --platform=$BUILDPLATFORM $BASE_IMAGE AS builder

RUN apk add --no-cache \
  ca-certificates \
  tzdata

FROM scratch

ARG BASE_IMAGE

LABEL \
  "com.centurylinklabs.watchtower"="true" \
  "org.opencontainers.image.url"="https://watchtower.nickfedor.com" \
  "org.opencontainers.image.documentation"="https://watchtower.nickfedor.com" \
  "org.opencontainers.image.source"="https://github.com/nicholas-fedor/watchtower" \
  "org.opencontainers.image.licenses"="Apache-2.0" \
  "org.opencontainers.image.title"="Watchtower" \
  "org.opencontainers.image.description"="A process for automating Docker container base image updates." \
  "org.opencontainers.image.base.name"="${BASE_IMAGE}"

# Copy ca-certs and timezone from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy binary (GoReleaser places the platform-specific binary at the context root as 'watchtower')
COPY watchtower /

EXPOSE 8080

HEALTHCHECK CMD ["/watchtower", "--health-check"]

ENTRYPOINT ["/watchtower"]
