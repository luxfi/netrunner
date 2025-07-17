# Build stage
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git make gcc musl-dev linux-headers bash

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build netrunner
RUN go build -o bin/netrunner .

# Runtime stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates curl bash

# Copy the binary
COPY --from=builder /build/bin/netrunner /usr/local/bin/netrunner

# Create netrunner user
RUN adduser -D -h /var/lib/netrunner netrunner

# Create directories
RUN mkdir -p /var/lib/netrunner && chown -R netrunner:netrunner /var/lib/netrunner

USER netrunner

EXPOSE 8080 8081

ENTRYPOINT ["/usr/local/bin/netrunner"]