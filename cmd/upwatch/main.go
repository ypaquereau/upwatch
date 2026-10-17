// Command upwatch is a URL monitoring service: an HTTP API plus a background checker.
package main

import "fmt"

// version is injected at build time: -ldflags "-X main.version=..."
var version = "dev"

func main() {
	fmt.Println("upwatch", version)
}
