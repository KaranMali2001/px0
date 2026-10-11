package main

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var rawVersion string

var version = strings.TrimSpace(rawVersion)
