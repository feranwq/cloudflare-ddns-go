FROM golang:1.19-alpine AS build_base

WORKDIR /tmp/build

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

RUN go build -o app cloudflare-ddns.go

FROM alpine:3.16

COPY --from=build_base /tmp/build/app /apps/app

CMD ["/apps/app"]
