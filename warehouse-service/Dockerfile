FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy the entire project with go.mod at root
COPY . .

# Build the specific service
RUN CGO_ENABLED=0 GOOS=linux go build -o warehouse-service ./warehouse-service

# Use a minimal alpine image
FROM alpine:3.18

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/warehouse-service .

# Expose port
EXPOSE 8081

# Set entry point
CMD ["./warehouse-service"]