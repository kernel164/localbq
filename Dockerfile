FROM golang:1.24-bookworm AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o /localbq -ldflags="-s -w" ./cmd/localbq/

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates && rm -rf /var/lib/apt/lists/*

COPY --from=builder /localbq /usr/local/bin/localbq

EXPOSE 9060
VOLUME /data

ENTRYPOINT ["localbq"]
CMD ["--port=9060", "--data-dir=/data"]
