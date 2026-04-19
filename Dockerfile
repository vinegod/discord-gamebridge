FROM golang:1.25-alpine AS builder
WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -ldflags "-s -w" -o discord-gamebridge .

FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /build/discord-gamebridge .

ENTRYPOINT ["/app/discord-gamebridge"]
CMD ["-config", "/config/config.yaml"]
