#!/usr/bin/env bash
# Regenerates internal/proto/external_interface from proto/gateway/*.proto.
# Run inside the vectro-protoc Docker image (see proto/protoc.Dockerfile).
set -euo pipefail
cd "$(dirname "$0")/.."
OUT=internal/proto/external_interface
PKG=vectro/cloud/internal/proto/external_interface
mkdir -p "$OUT"
cd proto/gateway
M=""
for f in *.proto; do M="$M,M$f=$PKG;external_interface"; done
M=${M#,}
protoc --proto_path=. --proto_path=/usr/include \
  --go_out=../../$OUT --go_opt=paths=source_relative,$M \
  --go-grpc_out=../../$OUT --go-grpc_opt=paths=source_relative,$M \
  --grpc-gateway_out=../../$OUT --grpc-gateway_opt=paths=source_relative,$M \
  *.proto
