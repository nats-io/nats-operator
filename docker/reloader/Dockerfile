FROM golang:1.13-alpine3.10 AS builder
WORKDIR $GOPATH/src/github.com/nats-io/nats-operator/
RUN apk add --update git
COPY . .
RUN go get ./...
RUN CGO_ENABLED=0 go build -ldflags "-X github.com/nats-io/nats-operator/version.GitSHA=`git rev-parse --short HEAD`" -installsuffix cgo -o /nats-server-config-reloader ./cmd/reloader/main.go

FROM alpine:3.8
COPY --from=builder /nats-server-config-reloader /usr/local/bin/nats-server-config-reloader
CMD ["nats-server-config-reloader"]
