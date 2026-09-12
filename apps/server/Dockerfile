FROM golang:1.24-alpine AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=sum.golang.google.cn
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/uplook-api ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/uplook-worker ./cmd/worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/uplook-migrate ./cmd/migrate
RUN mkdir -p /out/data/uploads

FROM alpine:3.22
RUN apk add --no-cache ca-certificates ffmpeg \
    && addgroup -S uplook \
    && adduser -S -G uplook uplook
WORKDIR /app
COPY --from=build --chown=uplook:uplook /out/uplook-api /app/uplook-api
COPY --from=build --chown=uplook:uplook /out/uplook-worker /app/uplook-worker
COPY --from=build --chown=uplook:uplook /out/uplook-migrate /app/uplook-migrate
COPY --from=build --chown=uplook:uplook /out/data /app/data
COPY --chown=uplook:uplook assets /app/assets
COPY --chown=uplook:uplook config /app/config
USER uplook
EXPOSE 8080
ENTRYPOINT ["/app/uplook-api"]
