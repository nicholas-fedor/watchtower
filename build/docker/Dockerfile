ARG BASE_IMAGE=alpine:3.22.1@sha256:4bcff63911fcb4448bd4fdacec207030997caf25e9bea4045fa6c8c44de311d1
ARG GOOS=linux
ARG GOARCH

FROM --platform=$BUILDPLATFORM $BASE_IMAGE AS builder

RUN apk add --no-cache \
  ca-certificates \
  tzdata

FROM scratch

LABEL "com.centurylinklabs.watchtower"="true"
LABEL "org.opencontainers.image.url"="https://nicholas-fedor.github.io/watchtower/" \
  "org.opencontainers.image.documentation"="https://nicholas-fedor.github.io/watchtower/" \
  "org.opencontainers.image.source"="https://github.com/nicholas-fedor/watchtower" \
  "org.opencontainers.image.licenses"="Apache-2.0" \
  "org.opencontainers.image.title"="Watchtower" \
  "org.opencontainers.image.description"="A process for automating Docker container base image updates." \
  "org.opencontainers.image.base.name"="${BASE_IMAGE:-alpine:3.22.1}"

# Copy ca-certs and timezone from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy binary (GoReleaser places the platform-specific binary at the context root as 'watchtower')
COPY watchtower /watchtower

EXPOSE 8080

HEALTHCHECK CMD ["/watchtower", "--health-check"]

ENTRYPOINT ["/watchtower"]
