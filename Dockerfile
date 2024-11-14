# Build stage
FROM golang:1.23.2-alpine AS builder

WORKDIR /app

# Add git and basic build tools
RUN apk add --no-cache git make build-base

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN go build -o main ./cmd/ghloner

# Final stage
FROM alpine:latest

# Copy the binary from builder
COPY --from=builder /app/main /main

# Command to run
ENTRYPOINT ["/main"] 