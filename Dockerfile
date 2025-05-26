# --- Build Arguments ---
ARG GO_VERSION=1.24
ARG ALPINE_VERSION=latest
ARG APP_NAME=gohub_server
ARG APP_WORKDIR=/app
ARG APP_CONFIG_FILE_PATH=/app/configs/config.yaml

# --- Builder Stage ---
FROM golang:${GO_VERSION}-alpine AS builder

# Set build arguments again for this stage
ARG APP_NAME
ARG APP_WORKDIR
ARG APP_CONFIG_FILE_PATH

WORKDIR ${APP_WORKDIR}

# Copy go.mod and go.sum files and download dependencies
COPY go.mod go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download && go mod verify

# Copy all source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -a -installsuffix cgo -o ${APP_WORKDIR}/${APP_NAME} ./cmd/gohub/main.go

# --- Final Stage ---
FROM alpine:${ALPINE_VERSION}

# Set build arguments again for this stage
ARG APP_NAME
ARG APP_WORKDIR
ARG APP_CONFIG_FILE_PATH

WORKDIR ${APP_WORKDIR}

# Create a non-root user and group
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Copy the compiled binary from the builder stage
COPY --from=builder ${APP_WORKDIR}/${APP_NAME} ${APP_WORKDIR}/${APP_NAME}

# Copy configs directory (optional, as it's usually mounted via docker-compose)
# This ensures the app can find a default config if the volume mount fails or is not present.
# COPY configs ./configs

# Ensure the appuser owns the app directory and binary
# And can read the config if it's copied into the image
RUN chown -R appuser:appgroup ${APP_WORKDIR}
# If you COPY configs above, also:
# RUN chown -R appuser:appgroup ${APP_WORKDIR}/configs

# Expose application port (make sure this matches your config.yaml)
EXPOSE 8080

# Switch to the non-root user
USER appuser

# Run the application
# The CMD now uses the ARG for the config file path
CMD ["./gohub_server", "-config", "/app/configs/config.yaml"] 