package main

import "fmt"

// version is injected at build time: -ldflags "-X main.version=..."
var version = "dev"

func main() {
	fmt.Println("upwatch", version)
}
