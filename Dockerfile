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
    git \
    && rm -rf /var/lib/apt/lists/* \
    && apt-get clean

WORKDIR /app

COPY --from=builder /build/cloud-agents-github-plugin /app/cloud-agents-github-plugin

RUN chmod +x /app/cloud-agents-github-plugin

ENTRYPOINT ["/app/cloud-agents-github-plugin"]
