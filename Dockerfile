# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /seed ./cmd/seed

# Runtime stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
COPY --from=builder /server /usr/local/bin/server
COPY --from=builder /seed /usr/local/bin/seed
COPY migrations /migrations

USER appuser
EXPOSE 8080
ENTRYPOINT ["server"]
