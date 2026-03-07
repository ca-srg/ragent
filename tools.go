//go:build tools

// Package tools tracks module dependencies used in sqlite-vec backend implementation.
// This file ensures go mod tidy retains these dependencies before they are imported
// in feature code.
package main

import (
	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces" // keep for go.mod dep tracking
)
