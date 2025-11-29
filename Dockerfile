# Pin base images by digest for supply chain security
# Renovate will automatically update these digests
FROM golang:1.25.0-alpine@sha256:f18a072054848d87a8077455f0ac8a25886f2397f88bfdd222d6fafbb5bba440 AS builder

# hadolint ignore=DL3018
RUN apk add --no-cache gcc musl-dev git

WORKDIR ${GOPATH}/src/github.com/netresearch/ofelia

COPY go.mod go.sum ./
RUN go mod download

COPY . ${GOPATH}/src/github.com/netresearch/ofelia

RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /go/bin/ofelia .

FROM alpine:3.21@sha256:5405e8f36ce1878720f71217d664aa3dea32e5e5df11acbf07fc78ef5661465b

# this label is required to identify container with ofelia running
LABEL ofelia.service=true
LABEL ofelia.enabled=true

# hadolint ignore=DL3018
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /go/bin/ofelia /usr/bin/ofelia

HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
  CMD pgrep ofelia >/dev/null || exit 1

ENTRYPOINT ["/usr/bin/ofelia"]

CMD ["daemon", "--config", "/etc/ofelia/config.ini"]
