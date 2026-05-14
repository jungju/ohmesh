FROM golang:1.24-alpine AS build

RUN apk add --no-cache build-base ca-certificates

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=1
RUN go build -ldflags="-s -w" -o /out/ohmesh ./cmd/ohmesh

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
	&& addgroup -S ohmesh \
	&& adduser -S -G ohmesh ohmesh \
	&& mkdir -p /data \
	&& chown -R ohmesh:ohmesh /data

USER ohmesh
WORKDIR /app

COPY --from=build /out/ohmesh /app/ohmesh

EXPOSE 8080

ENTRYPOINT ["/app/ohmesh"]
