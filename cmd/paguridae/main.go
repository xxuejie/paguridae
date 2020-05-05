package main // import "github.com/xxuejie/paguridae"

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/NYTimes/gziphandler"
	"nhooyr.io/websocket"
)

var port = flag.Int("port", 8000, "port to listen for http server")
var useLocalAsset = flag.Bool("useLocalAsset", false, "development only, you shouldn't use true in production")
var verifyContent = flag.Bool("verifyContent", false, "development only, set to true to enable content verification")

func webSocketHandler(w http.ResponseWriter, req *http.Request) {
	c, err := websocket.Accept(w, req, websocket.AcceptOptions{})
	if err != nil {
		log.Print("Error upgrading websocket:", err)
		return
	}
	defer c.Close(websocket.StatusInternalError, "oops")
	log.Print("Websocket connection established!")

	connection, err := NewConnection(*verifyContent)
	if err != nil {
		log.Print("Error creating connection:", err)
		return
	}
	err = connection.Serve(req.Context(), c)
	if err != nil {
		log.Print("Error serving connection:", err)
	}
	connection.Stop()
}

func main() {
	flag.Parse()
	http.HandleFunc("/ws", webSocketHandler)
	http.Handle("/", gziphandler.GzipHandler(http.FileServer(FS(*useLocalAsset))))
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		os.RemoveAll("/tmp/paguridae")
		os.Exit(0)
	}()
	log.Printf("Starting server on port: %d", *port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}
