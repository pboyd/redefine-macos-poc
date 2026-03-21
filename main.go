package main

import (
	"fmt"
	"os"
	"time"
)

func myTimeNow() time.Time {
	return time.Date(2026, 1, 30, 17, 0, 0, 0, time.FixedZone("Somewhere", 0))
}

func main() {
	err := redefineFunc(time.Now, myTimeNow)
	if err != nil {
		fmt.Fprintf(os.Stderr, "redefineFunc failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(time.Now().Format(time.Kitchen))
}
