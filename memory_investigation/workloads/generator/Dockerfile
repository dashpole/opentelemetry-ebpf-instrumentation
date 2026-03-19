FROM golang:1.24-alpine as builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o workload .

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/workload .
CMD ["./workload"]
