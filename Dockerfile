# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev

# Copy go.mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o smartsyncserver .

# Runtime stage
FROM alpine:latest

# Install ca-certificates for HTTPS git operations
RUN apk --no-cache add ca-certificates git

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/smartsyncserver .
COPY config.example.yaml /app/config.yaml

# Create vault directory
RUN mkdir /vault

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:8080/status || exit 1

# Run the application
CMD ["./smartsyncserver"]
