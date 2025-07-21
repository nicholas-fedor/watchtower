ARG BASE_IMAGE=alpine:3.22.1@sha256:4bcff63911fcb4448bd4fdacec207030997caf25e9bea4045fa6c8c44de311d1
ARG GOOS=linux
ARG GOARCH
ARG GOAMD64=v1
ARG GO386=sse2
ARG GOARM=6
ARG GOARM64=v8.0
ARG GORISCV64=rva20u64

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
  "org.opencontainers.image.base.name"="$BASE_IMAGE"

# Copy ca-certs and timezone from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy binary based on GOOS, GOARCH, and variant
COPY dist/watchtower_${GOOS}_${GOARCH}${GOAMD64:+_${GOAMD64}}${GO386:+_${GO386}}${GOARM:+_${GOARM}}${GOARM64:+_${GOARM64}}${GORISCV64:+_${GORISCV64}}/watchtower /watchtower

EXPOSE 8080

HEALTHCHECK CMD ["/watchtower", "--health-check"]

ENTRYPOINT ["/watchtower"]
