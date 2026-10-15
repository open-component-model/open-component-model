//go:build !pprof

package main

const pprofEnabled = false

func startPprof(_ string) {}
