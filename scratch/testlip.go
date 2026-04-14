package main

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
)

func main() {
	var s = "°C"
	fmt.Printf("lipgloss.Width: %d\n", lipgloss.Width(s))
}
