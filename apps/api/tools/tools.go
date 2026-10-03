//go:build tools
// +build tools

// Package tools tracks tool dependencies for go:generate and go install.
//
//go:generate go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
//go:generate go install github.com/air-verse/air@latest
//go:generate go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
package tools

import (
	_ "github.com/air-verse/air"
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
	_ "github.com/sqlc-dev/sqlc/cmd/sqlc"
)