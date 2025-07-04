ARG BASE_IMAGE=alpine:3.22.0@sha256:8a1f59ffb675680d47db6337b49d22281a139e9d709335b492be023728e11715

FROM --platform=$BUILDPLATFORM $BASE_IMAGE AS alpine

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

COPY --from=alpine \
    /etc/ssl/certs/ca-certificates.crt \
    /etc/ssl/certs/ca-certificates.crt
COPY --from=alpine \
    /usr/share/zoneinfo \
    /usr/share/zoneinfo

EXPOSE 8080

COPY watchtower /

HEALTHCHECK CMD [ "/watchtower", "--health-check"]

ENTRYPOINT ["/watchtower"]
