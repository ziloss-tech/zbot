# ── Stage 1: Build React frontend ─────────────────────────────────────────
FROM node:20-alpine AS frontend
WORKDIR /app/internal/webui/frontend
COPY internal/webui/frontend/package*.json ./
RUN npm ci
COPY internal/webui/frontend/ ./
RUN npm run build

# ── Stage 2: Build Go binary ─────────────────────────────────────────────
FROM golang:1.26-alpine AS builder
RUN apk add --no-cache git ca-certificates
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Copy freshly built frontend into the embed location
COPY --from=frontend /app/internal/webui/frontend/dist ./internal/webui/frontend/dist
RUN CGO_ENABLED=0 GOOS=linux go build -o /zbot ./cmd/zbot

# ── Stage 3: Minimal runtime ─────────────────────────────────────────────
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /zbot .
COPY config.example.yaml config.yaml
RUN mkdir -p workspace

ENV ZBOT_ENV=production
ENV ZBOT_BIND_ALL=true

EXPOSE 18790
CMD ["./zbot"]
