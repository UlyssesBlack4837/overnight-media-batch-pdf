package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/infrai-examples/overnight-media-digest/internal/digest"
)

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	service := digest.Service{Infrai: digest.NewClient(key), PollEvery: 5 * time.Second}
	server := &http.Server{Addr: ":8080", Handler: service.Handler(), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("media digest listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
