FROM golang:1.22-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -ldflags "-s -w" -o /bin/xmanager ./cmd/xmanager

FROM alpine:3.20

RUN apk add --no-cache ca-certificates openssh-client sqlite-libs
COPY --from=builder /bin/xmanager /usr/local/bin/xmanager

RUN ln -s /usr/local/bin/xmanager /usr/local/bin/vpsm

ENTRYPOINT ["xmanager"]
