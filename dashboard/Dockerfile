FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod main.go ./
COPY static ./static
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /dashboard .
FROM alpine:3.21
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 dashboard
COPY --from=build /dashboard /dashboard
USER dashboard
EXPOSE 8080
ENTRYPOINT ["/dashboard"]
