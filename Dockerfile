FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go schema.sql ./
RUN CGO_ENABLED=0 go build -o /gateway .

FROM alpine:3.23
RUN apk add --no-cache ca-certificates
COPY --from=build /gateway /gateway
USER 65532:65532
ENTRYPOINT ["/gateway"]
