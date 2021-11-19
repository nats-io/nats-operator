FROM golang:1.16-alpine3.14 AS builder
WORKDIR $GOPATH/src/github.com/nats-io/nats-operator/
RUN apk add --update git
COPY . .
RUN go get ./...
RUN CGO_ENABLED=0 go build -ldflags "-X github.com/nats-io/nats-operator/version.GitSHA=`git rev-parse --short HEAD`" -installsuffix cgo -o /nats-operator ./cmd/operator/main.go

FROM alpine:3.14
COPY --from=builder /nats-operator /usr/local/bin/nats-operator
CMD ["nats-operator"]
