# Build stage
FROM golang:1.23.2 AS build

WORKDIR /app

# Copy the Go module files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the application
RUN go build -o /workflow-ghcr-cleaner

# Final image
FROM alpine:latest

# Copy the built binary and entrypoint script
COPY --from=build /workflow-ghcr-cleaner /usr/local/bin/workflow-ghcr-cleaner
COPY entrypoint.sh /entrypoint.sh

# Make the entrypoint script executable
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
