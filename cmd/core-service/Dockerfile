FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/core-service ./cmd/core-service

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/core-service /app/core-service
COPY migrations /app/migrations
ENTRYPOINT ["/app/core-service"]
