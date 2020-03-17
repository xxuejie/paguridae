package main // import "github.com/xxuejie/paguridae"

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/NYTimes/gziphandler"
	"nhooyr.io/websocket"
)

var port = flag.Int("port", 8000, "port to listen for http server")
var useLocalAsset = flag.Bool("useLocalAsset", false, "development only, you shouldn't use true in production")

func webSocketHandler(w http.ResponseWriter, req *http.Request) {
	c, err := websocket.Accept(w, req, websocket.AcceptOptions{})
	if err != nil {
		log.Print("Error upgrading websocket:", err)
		return
	}
	defer c.Close(websocket.StatusInternalError, "oops")
	log.Print("Websocket connection established!")

	err = NewConnection().Serve(req.Context(), c)
	if err != nil {
		log.Print("Error serving connection:", err)
		return
	}
}

func main() {
	flag.Parse()
	http.HandleFunc("/ws", webSocketHandler)
	http.Handle("/", gziphandler.GzipHandler(http.FileServer(FS(*useLocalAsset))))
	log.Printf("Starting server on port: %d", *port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}
