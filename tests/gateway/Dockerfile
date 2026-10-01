# Acceptance-only stand-in for the embedding and chat gateways.
#
# Build:  docker build -t campusclaw-zwh-gateway:latest tests/gateway
# Run:    docker run -d --name campusclaw-zwh-stub-gateway \
#           -p 127.0.0.1:11434:8080 campusclaw-zwh-gateway:latest
#
# It is never part of compose.yaml: the product talks to a real gateway, and
# this exists only so the end-to-end acceptance run needs no model provider.

FROM golang:1.25-alpine AS build

WORKDIR /src
ENV CGO_ENABLED=0

COPY go.mod main.go ./
RUN go build -trimpath -ldflags="-s -w" -o /out/gateway main.go

FROM alpine:3.20

COPY --from=build /out/gateway /usr/local/bin/gateway

ENV PORT=8080 \
    EMBEDDING_DIMENSIONS=1536

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/gateway"]
