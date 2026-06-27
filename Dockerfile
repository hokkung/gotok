# --- Build stage ---
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Cache dependencies.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build.
COPY . .
RUN CGO_ENABLED=0 go build -o /gotok ./cmd/gotok

# --- Runtime stage ---
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /gotok /app/gotok
COPY web/ /app/web/

# Upload directory.
RUN mkdir -p /app/data/uploads

EXPOSE 8080

ENTRYPOINT ["/app/gotok"]
