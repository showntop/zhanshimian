FROM golang:1.24-alpine AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=sum.golang.google.cn
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/uplook-api ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/uplook-worker ./cmd/worker
RUN mkdir -p /out/data/uploads

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && addgroup -S jianwo \
    && adduser -S -G jianwo jianwo
WORKDIR /app
COPY --from=build --chown=jianwo:jianwo /out/uplook-api /app/uplook-api
COPY --from=build --chown=jianwo:jianwo /out/uplook-worker /app/uplook-worker
COPY --from=build --chown=jianwo:jianwo /out/data /app/data
COPY --chown=jianwo:jianwo assets /app/assets
COPY --chown=jianwo:jianwo config /app/config
USER jianwo
EXPOSE 8080
ENTRYPOINT ["/app/uplook-api"]
