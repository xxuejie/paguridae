package main // import "github.com/xxuejie/paguridae"

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/fmpwizard/go-quilljs-delta/delta"
	"nhooyr.io/websocket"
)

type Change struct {
	Id     int         `json:"id"`
	Change delta.Delta `json:"change"`
}

type Action struct {
	Id     int    `json:"id"`
	Action string `json:"action"`
	Index  int    `json:"index"`
	Length int    `json:"length"`
}

type Request struct {
	Changes []Change `json:"rows"`
	Action  Action   `json:"action"`
}

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

	changes := []Change{
		Change{
			Id:     0,
			Change: *delta.New(nil).Insert("1\n2\n3\n4", nil),
		},
		Change{
			Id:     1,
			Change: *delta.New(nil).Insert(" | New Newcol Cut Copy Paste", nil),
		},
		Change{
			Id:     2,
			Change: *delta.New(nil).Insert("Foobar\nLine 2\n\nAnotherLine", nil),
		},
		Change{
			Id:     3,
			Change: *delta.New(nil).Insert("~ | New Newcol Cut Copy Paste", nil),
		},
	}
	changesBytes, err := json.Marshal(changes)
	if err != nil {
		log.Print("Error marshaling json:", err)
		return
	}
	err = c.Write(req.Context(), websocket.MessageText, changesBytes)
	if err != nil {
		log.Print("Error sending initial change:", err)
		return
	}

	for {
		_, b, err := c.Read(req.Context())
		if err != nil {
			log.Print("Error reading message:", err)
			return
		}
		log.Print("Message: ", string(b))
		var request Request
		err = json.Unmarshal(b, &request)
		if err != nil {
			log.Print("Error unmarshaling message:", err)
			continue
		}

		log.Print("Request:", request)
	}
}

func main() {
	flag.Parse()
	http.HandleFunc("/ws", webSocketHandler)
	http.Handle("/", http.FileServer(FS(*useLocalAsset)))
	log.Printf("Starting server on port: %d", *port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}
