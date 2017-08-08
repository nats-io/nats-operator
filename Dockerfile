FROM golang:1.8.3-alpine3.6 AS builder
COPY ./ /go/src/github.com/pires/nats-operator
WORKDIR /go/src/github.com/pires/nats-operator
RUN apk add --update git
RUN go get -u github.com/golang/dep/cmd/dep
RUN dep ensure
RUN CGO_ENABLED=0 go build -installsuffix cgo -o /nats-operator ./cmd/operator/main.go

FROM alpine:3.6
COPY --from=builder /nats-operator /usr/local/bin/nats-operator
CMD ["nats-operator"]
