FROM golang:1.25.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/server ./cmd/server

FROM alpine:3.21
RUN addgroup -S app && adduser -S -G app app && mkdir -p /data/generated /data/secrets && chown -R app:app /data/generated /data/secrets
COPY --from=build /out/server /server
USER app
WORKDIR /app
EXPOSE 8080
ENTRYPOINT ["/server"]
