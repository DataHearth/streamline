package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(2)
	}
	switch args[0] {
	case "echo":
		fmt.Print(strings.Join(args[1:], " "))
	case "env":
		fmt.Print(strings.Join(os.Environ(), "\n"))
	case "pwd":
		wd, err := os.Getwd()
		if err != nil {
			os.Exit(2)
		}
		fmt.Print(wd)
	case "exit":
		n, _ := strconv.Atoi(args[1])
		os.Exit(n)
	case "sleep":
		fmt.Print("ready")
		time.Sleep(time.Hour)
	case "flood":
		chunk := []byte(strings.Repeat("x", 1024))
		for range 4 << 10 {
			_, _ = os.Stderr.Write(chunk)
		}
		fmt.Print("done")
	case "stream":
		n, _ := strconv.Atoi(args[1])
		chunk := []byte(strings.Repeat("s", 1024))
		for range n {
			_, _ = os.Stdout.Write(chunk)
			time.Sleep(50 * time.Millisecond)
		}
	}
}
