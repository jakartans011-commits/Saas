# Dockerfile
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build backend
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o saas-api ./cmd/saas-api

# Final stage
FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# web/ dan template portal sudah di-embed ke binary (go:embed)
COPY --from=builder /app/saas-api .

EXPOSE 8080

CMD ["./saas-api"]