FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy the entire project with go.mod at root
COPY . .

# Build the specific service
RUN CGO_ENABLED=0 GOOS=linux go build -o api-gateway ./api-gateway

# Use a minimal alpine image
FROM alpine:3.18

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/api-gateway .

# Expose port
EXPOSE 8000

# Set entry point
CMD ["./api-gateway"]