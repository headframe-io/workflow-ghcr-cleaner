FROM golang:1.23.2 AS build

WORKDIR /app

COPY . .

RUN go build -o /workflow-ghcr-cleaner

FROM alpine:latest

COPY --from=build /workflow-ghcr-cleaner /usr/local/bin/workflow-ghcr-cleaner
COPY entrypoint.sh /entrypoint.sh

RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
