FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY main.go .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/dnsmock ./main.go

FROM alpine:3.20
RUN adduser -D -u 10001 app
USER app
COPY --from=builder /out/dnsmock /usr/local/bin/dnsmock
ENTRYPOINT ["/usr/local/bin/dnsmock"]
