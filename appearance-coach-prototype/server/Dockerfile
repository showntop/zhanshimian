FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/jianwo-api ./cmd/api
RUN mkdir -p /out/data/uploads

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && addgroup -S jianwo \
    && adduser -S -G jianwo jianwo
WORKDIR /app
COPY --from=build --chown=jianwo:jianwo /out/jianwo-api /app/jianwo-api
COPY --from=build --chown=jianwo:jianwo /out/data /app/data
COPY --chown=jianwo:jianwo assets /app/assets
USER jianwo
EXPOSE 8080
ENTRYPOINT ["/app/jianwo-api"]
