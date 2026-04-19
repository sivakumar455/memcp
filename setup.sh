#!/usr/bin/env bash
set -e

# Build and run the setup script
cd "$(dirname "$0")"
go run scripts/setup.go
