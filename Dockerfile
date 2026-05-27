# syntax=docker/dockerfile:1

FROM golang:1.26-alpine3.22 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/discord-gobot .

FROM alpine:3.22

RUN apk add --no-cache \
        ca-certificates \
        curl \
        font-dejavu \
        imagemagick \
        imagemagick-svg \
    && addgroup -S bot \
    && adduser -S -G bot bot

COPY --from=build /out/discord-gobot /usr/local/bin/discord-gobot

USER bot

ENTRYPOINT ["discord-gobot"]
