FROM golang:1.25-alpine AS builder
WORKDIR /build

RUN apk add --no-cache git

COPY core-users/go.mod core-users/go.sum ./core-users/
COPY core-health/go.mod core-health/go.sum ./core-health/
COPY public-max-bot/go.mod public-max-bot/go.sum ./public-max-bot/

WORKDIR /build/public-max-bot
RUN go mod download

WORKDIR /build
COPY core-users/ ./core-users/
COPY core-health/ ./core-health/
COPY public-max-bot/ ./public-max-bot/

WORKDIR /build/public-max-bot
RUN CGO_ENABLED=0 go build -o /app/public-max-bot ./cmd/public-max-bot

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/public-max-bot .
COPY --from=builder /build/public-max-bot/config/configs_keys.yml ./config/configs_keys.yml
EXPOSE 8082
CMD ["./public-max-bot"]
