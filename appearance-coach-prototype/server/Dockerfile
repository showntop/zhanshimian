FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/jianwo-api ./cmd/api
RUN mkdir -p /out/data/uploads

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/jianwo-api /app/jianwo-api
COPY --from=build --chown=65532:65532 /out/data /app/data
COPY --chown=65532:65532 assets /app/assets
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/jianwo-api"]
