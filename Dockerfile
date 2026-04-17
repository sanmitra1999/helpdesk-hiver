FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/helpdesk ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /bin/helpdesk /app/helpdesk
VOLUME ["/data"]
EXPOSE 8080
CMD ["/app/helpdesk"]
