package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	keyBytes, err := os.ReadFile("private_key.pem")
	if err != nil {
		panic(err)
	}
	// trim spaces/newlines
	keyStr := strings.TrimSpace(string(keyBytes))
	// replace actual newlines with literal \n
	formatted := strings.ReplaceAll(keyStr, "\n", "\\n")
	fmt.Println(formatted)
}
