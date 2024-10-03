# Build stage
FROM golang:1.23.2 AS build

WORKDIR /app

# If you don't have go.mod and go.sum, initialize a module
# RUN go mod init github.com/your-module-name

# Copy the source code
COPY . .

# Optionally, run 'go mod tidy' if you're using modules
# RUN go mod tidy

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
