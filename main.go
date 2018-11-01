package main // import "github.com/xxuejie/paguridae"

import (
	"flag"
	"fmt"
	"log"
	"net/http"
)

var port = flag.Int("port", 8000, "port to listen for http server")
var useLocalAsset = flag.Bool("useLocalAsset", false, "development only, you shouldn't use true in production")

func main() {
	flag.Parse()
	http.Handle("/", http.FileServer(FS(*useLocalAsset)))
	log.Printf("Starting server on port: %d", *port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}
