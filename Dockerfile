# Build stage
FROM golang:1.23-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    git make gcc libc6-dev bash \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build netrunner with static linking for portability
RUN CGO_ENABLED=0 go build -o bin/netrunner .

# Runtime stage - use debian-slim for better QEMU compatibility
FROM debian:12-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

# Copy the binary
COPY --from=builder /build/bin/netrunner /usr/local/bin/netrunner

# Create netrunner user
RUN useradd -r -m -d /var/lib/netrunner netrunner

# Create directories
RUN mkdir -p /var/lib/netrunner && chown -R netrunner:netrunner /var/lib/netrunner

USER netrunner

EXPOSE 8080 8081

ENTRYPOINT ["/usr/local/bin/netrunner"]