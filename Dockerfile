FROM golang:1.24-alpine AS builder

RUN apk update
RUN apk upgrade
RUN apk add gcc musl-dev git

WORKDIR ${GOPATH}/src/github.com/netresearch/ofelia

COPY go.mod go.sum ./
RUN go mod download

COPY . ${GOPATH}/src/github.com/netresearch/ofelia

RUN go build -o /go/bin/ofelia .

FROM alpine:3.21

# this label is required to identify container with ofelia running
LABEL ofelia.service=true
LABEL ofelia.enabled=true

RUN apk --no-cache upgrade
RUN apk --no-cache add ca-certificates tzdata

COPY --from=builder /go/bin/ofelia /usr/bin/ofelia

HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
  CMD pgrep ofelia >/dev/null || exit 1

ENTRYPOINT ["/usr/bin/ofelia"]

CMD ["daemon", "--config", "/etc/ofelia/config.ini"]
