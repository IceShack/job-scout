FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/scraper ./cmd/scraper

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 scraper
USER scraper
WORKDIR /app
COPY --from=build /out/scraper /app/scraper
# The example profile ships as the default so the image runs unconfigured;
# real deployments mount their own over it (see CONFIG_PATH).
COPY config.example.yaml /app/config.yaml
ENV STATE_DIR=/data PORT=8080
EXPOSE 8080
ENTRYPOINT ["/app/scraper"]
