package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	addr := ":18080"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	fmt.Printf("go-http-srv listening on %s (pid=%d)\n", addr, os.Getpid())
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
