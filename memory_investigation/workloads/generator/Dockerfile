FROM golang:1.24-alpine@sha256:0d9d43d52c6f1a8c0819fc7d1f50a8c4fa4c959ecdddedc9342ecbc5845c4709 as builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o workload .

FROM alpine:3.21.3@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c
WORKDIR /root/
COPY --from=builder /app/workload .
CMD ["./workload"]
