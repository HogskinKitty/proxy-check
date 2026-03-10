FROM golang:1.22 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/proxy-check

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=builder /out/proxy-check /app/proxy-check
EXPOSE 8080
ENTRYPOINT ["/app/proxy-check","--server","--listen",":8080"]
