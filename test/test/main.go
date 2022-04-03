package main

import "fmt"

type T interface {
}

func main() {
	fmt.Printf("type:%T\n", (T)(nil))
	fmt.Printf("type:%T\n", (*error)(nil))
	fmt.Printf("type:%T\n", (error)(nil))
	var a error
	var b int
	fmt.Printf("type:%T\n", a)
	fmt.Printf("type:%T\n", b)
}
