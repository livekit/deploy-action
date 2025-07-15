FROM golang:1.24 AS builder

WORKDIR /build


RUN apt-get update && apt-get install -y git build-essential
COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY . .
ENV CGO_ENABLED=0 
ENV GOOS=linux 
ENV GOARCH=amd64
RUN go build -o cloud-agents-github-plugin ./cmd/cloud-agents-github-plugin


FROM debian:stable-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /build/cloud-agents-github-plugin /usr/local/bin/cloud-agents-github-plugin

RUN chmod +x /usr/local/bin/cloud-agents-github-plugin
ENTRYPOINT [ "cloud-agents-github-plugin" ]
