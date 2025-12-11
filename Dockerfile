# Build stage - Install Go 1.25.5 from source
FROM debian:bookworm-slim AS go-installer

RUN apt-get update && apt-get install -y --no-install-recommends \
    wget ca-certificates \
    && rm -rf /var/lib/apt/lists/*

ARG GO_VERSION=1.25.5
ARG TARGETARCH

RUN wget -q "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" \
    && tar -C /usr/local -xzf "go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" \
    && rm "go${GO_VERSION}.linux-${TARGETARCH}.tar.gz"

# Build stage
FROM debian:bookworm-slim AS builder

COPY --from=go-installer /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"

RUN apt-get update && apt-get install -y --no-install-recommends \
    git make gcc libc6-dev ca-certificates \
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
