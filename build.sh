#!/bin/bash
set -e

echo "Building portal..."
CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o portal ./cmd/server
echo "Build complete: ./portal"
