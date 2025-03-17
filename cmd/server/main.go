package main

import (
	"fmt"
	"net/http"
)

const Port = ":8080"

func main() {
	fmt.Println("Starting server on port", Port)
	err := http.ListenAndServe(Port, http.FileServer(http.Dir("../../assets")))
	if err != nil {
		fmt.Println("Failed to start server", err)
		return
	}
	fmt.Println("Started server on port ", Port)
}
