package main // import "github.com/xxuejie/paguridae"

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var port = flag.Int("port", 8000, "port to listen for http server")
var useLocalAsset = flag.Bool("useLocalAsset", false, "development only, you shouldn't use true in production")

var upgrader = websocket.Upgrader{}

func webSocketHandler(w http.ResponseWriter, req *http.Request) {
	_, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		log.Print("Error upgrading websocket:", err)
		return
	}
	log.Print("Websocket connection established!")
}

func main() {
	flag.Parse()
	http.HandleFunc("/ws", webSocketHandler)
	http.Handle("/", http.FileServer(FS(*useLocalAsset)))
	log.Printf("Starting server on port: %d", *port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}
