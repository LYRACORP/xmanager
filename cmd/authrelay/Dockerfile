FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -trimpath -o /relay .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /relay /relay
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://localhost:8090/health || exit 1
ENTRYPOINT ["/relay"]
