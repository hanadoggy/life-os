# syntax=docker/dockerfile:1

FROM golang:alpine AS builder

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /src

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOTOOLCHAIN=auto

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/server /server

ENV TZ=Asia/Tokyo \
    PORT=8080

EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/server"]

