# syntax=docker/dockerfile:1@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6
FROM golang:1.25.4-alpine3.22@sha256:d3f0cf7723f3429e3f9ed846243970b20a2de7bae6a5b66fc5914e228d831bbb
ARG GOOS=linux
ARG TARGETARCH

RUN apk add --no-cache \
                git \
                nano \
                openssh

ARG USERNAME=vscode
ARG USER_UID=1000
ARG USER_GID=1000
ENV GO111MODULE=on


# Setup user
RUN adduser $USERNAME -s /bin/sh -D -u $USER_UID $USER_GID && \
    mkdir -p /etc/sudoers.d && \
    echo $USERNAME ALL=\(root\) NOPASSWD:ALL > /etc/sudoers.d/$USERNAME && \
    chmod 0440 /etc/sudoers.d/$USERNAME

# Install golangci-lint
RUN wget -O- https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b /usr/local/bin/ v2.5.0

# Install mockery
# Map Docker's TARGETARCH (amd64/arm64) to Mockery's naming (x86_64/arm64)
RUN case ${TARGETARCH} in \
        amd64) ARCH="x86_64" ;; \
        arm64) ARCH="arm64" ;; \
        *) ARCH=${TARGETARCH} ;; \
    esac && \
    wget -O /tmp/mockery.tar.gz https://github.com/vektra/mockery/releases/download/v3.5.5/mockery_3.5.5_Linux_${ARCH}.tar.gz && \
    tar -xzf /tmp/mockery.tar.gz -C /tmp && \
    mv /tmp/mockery /usr/local/bin/mockery

USER $USERNAME

RUN go install -v golang.org/x/tools/gopls@latest
RUN go install -v github.com/go-delve/delve/cmd/dlv@latest
RUN go install -v honnef.co/go/tools/cmd/staticcheck@latest


