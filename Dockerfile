# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /seed ./cmd/seed

# Runtime stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /server /usr/local/bin/server
COPY --from=builder /seed /usr/local/bin/seed
COPY migrations /migrations

EXPOSE 8080
ENTRYPOINT ["server"]
