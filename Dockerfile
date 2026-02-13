# Binary selector stage — picks the correct pre-built binary for the target platform.
# Docker automatically sets TARGETARCH and TARGETVARIANT during multi-platform builds.
# All pre-built binaries must be in bin/ in the build context.
FROM alpine:3.23@sha256:51183f2cfa6320055da30872f211093f9ff1d3cf06f39a0bdb212314c5dc7375 AS binary-selector

ARG TARGETARCH
ARG TARGETVARIANT

COPY bin/ofelia-linux-* /tmp/

RUN set -eux; \
  if [ "${TARGETARCH}" = "arm" ]; then \
    BINARY="ofelia-linux-arm${TARGETVARIANT}"; \
  else \
    BINARY="ofelia-linux-${TARGETARCH}"; \
  fi; \
  cp "/tmp/${BINARY}" /usr/bin/ofelia; \
  chmod +x /usr/bin/ofelia

# Runtime stage
FROM alpine:3.23@sha256:51183f2cfa6320055da30872f211093f9ff1d3cf06f39a0bdb212314c5dc7375

# OCI Image Annotations
# See: https://github.com/opencontainers/image-spec/blob/main/annotations.md
# Dynamic labels (created, version, revision) are added by docker/metadata-action in CI
LABEL org.opencontainers.image.title="Ofelia" \
      org.opencontainers.image.description="A docker job scheduler (based on mcuadros/ofelia)" \
      org.opencontainers.image.url="https://github.com/netresearch/ofelia" \
      org.opencontainers.image.documentation="https://github.com/netresearch/ofelia#readme" \
      org.opencontainers.image.source="https://github.com/netresearch/ofelia" \
      org.opencontainers.image.vendor="Netresearch DTT GmbH" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.authors="Netresearch DTT GmbH <info@netresearch.de>" \
      org.opencontainers.image.base.name="alpine:3.23"

# This label is required to identify container with ofelia running
LABEL ofelia.service=true \
      ofelia.enabled=true

# tini is used as init process (PID 1) to properly reap zombie processes
# from local jobs. See: https://github.com/krallin/tini
# hadolint ignore=DL3018
RUN apk add --no-cache ca-certificates tini tzdata

COPY --from=binary-selector /usr/bin/ofelia /usr/bin/ofelia

HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
  CMD pgrep ofelia >/dev/null || exit 1

# Use tini as init to handle zombie process reaping
# The -g flag ensures tini kills the entire process group on signal
ENTRYPOINT ["/sbin/tini", "-g", "--", "/usr/bin/ofelia"]

CMD ["daemon", "--config", "/etc/ofelia/config.ini"]
