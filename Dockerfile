FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /app/bot-converter ./main.go

FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    ffmpeg \
    imagemagick \
    libreoffice \
    calibre \
    poppler-utils \
    fonts-liberation \
    fonts-dejavu-core \
    fonts-noto \
    fonts-noto-cjk \
    fonts-noto-color-emoji \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /app/temp
ENV TMPDIR=/app/temp
ENV TMP=/app/temp
ENV TEMP=/app/temp

WORKDIR /app

COPY --from=builder /app/bot-converter /app/bot-converter

RUN chmod +x /app/bot-converter

CMD ["/app/bot-converter"]

