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

// A row is just an editor window, we call it a row since in it you get
// actually 2 editors: a label editor, and a content editor
type Row struct {
	Id      int          `json:"id"`
	Label   *delta.Delta `json:"label,omitempty"`
	Content *delta.Delta `json:"content,omitempty"`
}

type Action struct {
	Id     int    `json:"id"`
	Action string `json:"action"`
	Type   string `json:"type"`
	Index  int    `json:"index"`
	Length int    `json:"length"`
}

type Request struct {
	Rows   []Row  `json:"rows"`
	Action Action `json:"action"`
}

// All deltas included in this struct(included nested ones) are optional,
// if one is missing, it means no change is made on this part.
type Change struct {
	Layout *delta.Delta `json:"layout,omitempty"`
	Rows   []Row        `json:"rows,omitempty"`
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

	initialChange := Change{
		Layout: delta.New(nil).Insert("1\n2", nil),
		Rows: []Row{
			Row{
				Id:      1,
				Label:   delta.New(nil).Insert(" | New Newcol Cut Copy Paste", nil),
				Content: delta.New(nil).Insert("Foobar\nLine 2\n\nAnotherLine", nil),
			},
			Row{
				Id:      2,
				Label:   delta.New(nil).Insert("~ | New Newcol Cut Copy Paste", nil),
				Content: nil,
			},
		},
	}
	initialChangeBytes, err := json.Marshal(initialChange)
	if err != nil {
		log.Print("Error marshaling json:", err)
		return
	}
	err = c.Write(req.Context(), websocket.MessageText, initialChangeBytes)
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
