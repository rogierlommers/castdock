FROM golang:1.27-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/castdock ./cmd/castdock

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S castdock \
    && adduser -S -G castdock castdock \
    && mkdir -p /data \
    && chown castdock:castdock /data
COPY --from=build /out/castdock /usr/local/bin/castdock

USER castdock
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/castdock"]
