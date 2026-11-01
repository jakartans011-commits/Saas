package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
)

//go:embed static/*
var staticFS embed.FS

func main() {
	// Serve static files
	static, _ := fs.Sub(staticFS, "static")
	http.Handle("/", http.FileServer(http.FS(static)))

	log.Println("Swagger UI Playground running on http://localhost:8091")
	log.Fatal(http.ListenAndServe(":8091", nil))
}
