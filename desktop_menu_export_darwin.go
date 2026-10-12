//go:build desktop && darwin

package main

import "C"

//export px0MenuSettings
func px0MenuSettings() {
	go emitMenu("settings")
}
