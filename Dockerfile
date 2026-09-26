FROM golang:1.27 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/bot ./cmd/bot
RUN CGO_ENABLED=0 go build -o /out/fetcher ./cmd/fetcher
RUN CGO_ENABLED=0 go build -o /out/curate ./cmd/curate
RUN CGO_ENABLED=0 go build -o /out/exchange-service ./cmd/exchange-service
RUN CGO_ENABLED=0 go build -o /out/gateway ./cmd/gateway

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -u 1000 -m bot
WORKDIR /app
COPY --from=builder /out/bot ./bot
COPY --from=builder /out/fetcher ./fetcher
COPY --from=builder /out/curate ./curate
COPY --from=builder /out/exchange-service ./exchange-service
COPY --from=builder /out/gateway ./gateway

USER bot
CMD ["./bot"]
