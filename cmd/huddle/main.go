// Package main is the huddle MCP server entry point.
//
// Today this binary is a version-print stub. The foundation stream
// wires the modelcontextprotocol/go-sdk server, registers the v0 tool
// verbs, and starts the stdio loop.
package main

import "fmt"

const version = "0.0.0"

func main() {
	fmt.Printf("huddle %s\n", version)
}
