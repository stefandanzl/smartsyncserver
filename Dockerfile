# Build stage
FROM --platform=linux/amd64 golang:1.23-alpine AS builder
RUN apk --no-cache add git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/smartsyncserver .

# Runtime stage
FROM --platform=linux/amd64 alpine:latest
RUN apk --no-cache add ca-certificates git tzdata
WORKDIR /app
COPY --from=builder /out/smartsyncserver /app/smartsyncserver
COPY config.example.yaml /app/config.yaml
RUN mkdir /vault
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:8080/status || exit 1
CMD ["./smartsyncserver"]