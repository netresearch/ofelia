# Pin base images by digest for supply chain security
# Renovate will automatically update these digests
FROM golang:1.25.5-alpine@sha256:26111811bc967321e7b6f852e914d14bede324cd1accb7f81811929a6a57fea9 AS builder

# hadolint ignore=DL3018
RUN apk add --no-cache gcc musl-dev git

WORKDIR ${GOPATH}/src/github.com/netresearch/ofelia

COPY go.mod go.sum ./
RUN go mod download

COPY . ${GOPATH}/src/github.com/netresearch/ofelia

RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /go/bin/ofelia .

FROM alpine:3.21@sha256:5405e8f36ce1878720f71217d664aa3dea32e5e5df11acbf07fc78ef5661465b

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
      org.opencontainers.image.base.name="alpine:3.21"

# This label is required to identify container with ofelia running
LABEL ofelia.service=true \
      ofelia.enabled=true

# hadolint ignore=DL3018
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /go/bin/ofelia /usr/bin/ofelia

HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
  CMD pgrep ofelia >/dev/null || exit 1

ENTRYPOINT ["/usr/bin/ofelia"]

CMD ["daemon", "--config", "/etc/ofelia/config.ini"]
