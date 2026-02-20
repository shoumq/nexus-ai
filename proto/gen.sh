#!/usr/bin/env sh
set -e

protoc \
  -I proto \
  --go_out=paths=source_relative:proto \
  --go-grpc_out=paths=source_relative:proto \
  proto/nexusai/v1/analyzer.proto
