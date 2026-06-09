FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/pulse-injestor \
    ./cmd/pulse-docker

FROM alpine:3.22

RUN apk add --no-cache btrfs-progs ca-certificates
COPY --from=build /out/pulse-injestor /usr/local/bin/pulse-injestor

ENTRYPOINT ["/usr/local/bin/pulse-injestor"]
CMD ["run"]
