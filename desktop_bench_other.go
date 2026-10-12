//go:build !desktop

package main

import "net/http"

func serveBenchResult(http.ResponseWriter, *http.Request) bool { return false }
