FROM golang:1.24 AS builder

WORKDIR /build

RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0 
ENV GOOS=linux 
ENV GOARCH=amd64
RUN go build -ldflags="-w -s" -o cloud-agents-github-plugin .


FROM debian:stable-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && apt-get clean

RUN groupadd -r appuser && useradd -r -g appuser appuser

WORKDIR /app

COPY --from=builder /build/cloud-agents-github-plugin /app/cloud-agents-github-plugin

RUN chown -R appuser:appuser /app && \
    chmod +x /app/cloud-agents-github-plugin

ENTRYPOINT ["sh", "-c", "chown -R appuser:appuser /workspace && exec su appuser -c '/app/cloud-agents-github-plugin'"]
