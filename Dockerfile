FROM golang:1.25.9 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/kuroshio ./cmd/kuroshio

FROM gcr.io/distroless/base-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/kuroshio /usr/local/bin/kuroshio

EXPOSE 2525 587 8080 9090

ENTRYPOINT ["/usr/local/bin/kuroshio"]
CMD ["-config", "/etc/kuroshio/config.yaml"]
