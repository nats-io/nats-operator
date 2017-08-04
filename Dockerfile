FROM golang:1.8.3-alpine3.6 AS builder
COPY ./ /go/src/github.com/pires/nats-operator
WORKDIR /go/src/github.com/pires/nats-operator
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o /main ./cmd/operator/main.go

FROM alpine:3.6
COPY --from=builder /main /usr/local/bin/nats-operator
CMD ["nats-operator"]
