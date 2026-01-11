FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o telegram-digest-bot ./cmd/digest-bot/main.go

FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/telegram-digest-bot .

ENTRYPOINT ["./telegram-digest-bot"]
