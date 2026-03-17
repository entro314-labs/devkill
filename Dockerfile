# devkill Docker Image - Multi-stage build
# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build arguments for version info
ARG BUILD_VERSION=dev
ARG BUILD_DATE
ARG GIT_COMMIT

# Build the binary
RUN CGO_ENABLED=0 go build -v -trimpath \
    -ldflags="-s -w -X main.version=${BUILD_VERSION} -X main.date=${BUILD_DATE} -X main.commit=${GIT_COMMIT}" \
    -o devkill \
    .

# Runtime stage
FROM alpine:3.23

# Install runtime dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Create non-root user
RUN adduser -D -s /bin/sh devkill

# Copy the binary from builder
COPY --from=builder /build/devkill /usr/local/bin/devkill

# Ensure binary is executable
RUN chmod +x /usr/local/bin/devkill

# Switch to non-root user
USER devkill

# Set working directory
WORKDIR /workspace

# Add labels for better container management
LABEL org.opencontainers.image.title="devkill"
LABEL org.opencontainers.image.description="A modern TUI to find and delete heavy dev artifacts across languages and platforms"
LABEL org.opencontainers.image.source="https://github.com/entro314-labs/devkill"
LABEL org.opencontainers.image.url="https://github.com/entro314-labs/devkill"
LABEL org.opencontainers.image.vendor="entro314-labs"

# Default command
ENTRYPOINT ["/usr/local/bin/devkill"]
CMD ["--help"]