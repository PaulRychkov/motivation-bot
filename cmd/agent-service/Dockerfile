FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/agent-service ./cmd/agent-service

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/agent-service /app/agent-service
COPY prompts /app/prompts
ENTRYPOINT ["/app/agent-service"]
