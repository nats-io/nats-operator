FROM golang:1.11-alpine3.8 AS builder
WORKDIR $GOPATH/src/github.com/nats-io/nats-operator/
RUN apk add --update git
RUN go get -u github.com/golang/dep/cmd/dep
COPY . .
RUN dep ensure -v -vendor-only
RUN CGO_ENABLED=0 go build -o /nats-pod-bootconfig ./cmd/bootconfig/main.go

FROM alpine:3.8
COPY --from=builder /nats-pod-bootconfig /usr/local/bin/nats-pod-bootconfig
CMD ["nats-pod-bootconfig"]
