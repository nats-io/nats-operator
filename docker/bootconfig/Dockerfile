FROM golang:1.16-alpine3.14 AS builder
WORKDIR $GOPATH/src/github.com/nats-io/nats-operator/
RUN apk add --update git
COPY . .
RUN go get ./...
RUN CGO_ENABLED=0 go build -o /nats-pod-bootconfig ./cmd/bootconfig/main.go

FROM alpine:3.14
COPY --from=builder /nats-pod-bootconfig /usr/local/bin/nats-pod-bootconfig
CMD ["nats-pod-bootconfig"]
