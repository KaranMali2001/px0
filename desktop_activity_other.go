//go:build desktop && !darwin

package main

func userActivity() func() { return func() {} }
