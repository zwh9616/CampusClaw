# Test runner image: the Go toolchain plus the same document parsers the API
# image ships, so the PDF and DOCX suites exercise the real code path instead
# of a stand-in.
#
# Build:  docker build -t campusclaw-zwh-test:latest backend/tests
FROM golang:1.25-alpine

# poppler-utils provides pdfinfo/pdftotext; poppler-data provides the predefined
# CJK CMaps the extractor needs for Chinese text.
RUN apk add --no-cache poppler-utils poppler-data

ENV CGO_ENABLED=0
