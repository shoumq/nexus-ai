FROM golang:1.25.0-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN mkdir -p /app/bin && go build -o /app/bin/nexus .

FROM alpine:3.20

WORKDIR /app

COPY --from=build /app/bin/nexus /app/nexus

EXPOSE 8080

ENV PORT=8080

ENTRYPOINT ["/app/nexus"]
