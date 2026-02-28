FROM golang:1.25.7-alpine AS build

WORKDIR /app

COPY nexus-ai/go.mod nexus-ai/go.sum ./nexus-ai/
COPY auth_service/go.mod auth_service/go.sum ./auth_service/
RUN cd /app/nexus-ai && go mod download

COPY nexus-ai ./nexus-ai
COPY auth_service ./auth_service
RUN mkdir -p /app/bin && cd /app/nexus-ai && go build -o /app/bin/nexus .

FROM alpine:3.20

WORKDIR /app

COPY --from=build /app/bin/nexus /app/nexus
COPY nexus-ai/migrations /app/migrations

EXPOSE 9091

ENV GRPC_ADDR=:9091

ENTRYPOINT ["/app/nexus"]
