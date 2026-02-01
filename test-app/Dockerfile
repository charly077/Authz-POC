FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod ./
COPY main.go ./
COPY internal/ ./internal/
RUN go build -o server .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/server .
COPY internal/templates/*.html ./internal/templates/
RUN mkdir -p /data /shared
CMD ["./server"]
