FROM golang:1.27.1-alpine3.24 AS build
RUN apk add --no-cache build-base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -tags nomsgpack -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && go build -tags nomsgpack -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker \
 && go build -tags nomsgpack -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate \
 && go build -tags nomsgpack -trimpath -ldflags="-s -w" -o /out/staff ./cmd/staff
FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata libstdc++ \
 && addgroup -S shopnext && adduser -S -G shopnext -u 10001 shopnext \
 && mkdir -p /app/data/uploads && chown -R shopnext:shopnext /app
WORKDIR /app
COPY --from=build /out/ /usr/local/bin/
COPY docs/ ./docs/
USER shopnext
EXPOSE 8080
CMD ["api"]
