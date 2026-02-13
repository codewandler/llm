package main

import "github.com/codewandler/llm/provider"

func main() {
	for _, m := range provider.AllModels() {
		println(m.ID)
	}
}
