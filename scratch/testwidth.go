package main

import (
	"fmt"
	"github.com/mattn/go-runewidth"
)

func main() {
	var s = "°C"
	fmt.Printf("runewidth: %d\n", runewidth.StringWidth(s))
}
