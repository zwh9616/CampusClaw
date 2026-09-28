# --- build -------------------------------------------------------------------
FROM golang:1.25-alpine AS build

WORKDIR /src

# The module proxy is a build argument so a mirrored or offline environment can
# override it without editing this file.
#
# The entries are separated by "|", not ",". A comma only falls through on a
# 404 or 410, so an unreachable proxy would abort the build; "|" falls through
# on any error, including a connection failure or a hang.
ARG GOPROXY=https://goproxy.cn|https://proxy.golang.org
ENV GOPROXY=${GOPROXY} \
    CGO_ENABLED=0

# Dependencies first so they stay cached when only the source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# --- runtime -----------------------------------------------------------------
FROM alpine:3.20

# poppler-utils provides pdfinfo and pdftotext, which the API invokes directly.
# poppler-data supplies the predefined CJK CMaps Chinese PDFs need. The API
# refuses to start if either command is missing, so a missing tool can never be
# mistaken for a corrupt document.
RUN apk add --no-cache \
        ca-certificates \
        poppler-utils \
        poppler-data \
        tzdata

# The process runs unprivileged and only ever writes inside its upload volume.
RUN addgroup -S -g 10001 campusclaw \
 && adduser -S -u 10001 -G campusclaw campusclaw \
 && mkdir -p /uploads \
 && chown campusclaw:campusclaw /uploads

COPY --from=build /out/api /usr/local/bin/api

USER campusclaw

EXPOSE 8081

ENTRYPOINT ["/usr/local/bin/api"]
