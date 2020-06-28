package main // import "github.com/xxuejie/paguridae"

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/mssola/user_agent"
	"golang.org/x/crypto/acme/autocert"
	"nhooyr.io/websocket"
)

var port = flag.Int("port", 8000, "port to listen for http server")
var customStyleFile = flag.String("customStyleFile", "", "file containing custom styles to apply, notice this won't work when useLocalAsset is true")
var useLocalAsset = flag.Bool("useLocalAsset", false, "development only, you shouldn't use true in production")
var verifyContent = flag.Bool("verifyContent", false, "development only, set to true to enable content verification")
var useHttps = flag.Bool("useHttps", false, "listen on 443 port for HTTPS requests")
var domain = flag.String("domain", "", "Domain to use for generating letsencrypt certificates")
var certCache = flag.String("certCache", "./certs", "Cached directory for certificates")
var redirectToHttps = flag.Bool("redirectoToHttps", true, "Redirect HTTP request to HTTPS request, only enabled when useHttps is also enabled")

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

// HTTPS handling logic is adapted from https://github.com/kjk/go-cookbook/blob/13bbc271f500ec28f21ebc28b82ac985b7e4bffd/free-ssl-certificates/main.go
func makeServerFromMux(mux *http.ServeMux) *http.Server {
	return &http.Server{
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
		Handler:      mux,
	}
}

var SupportedBrowsers = map[string]bool{
	"Chrome": true,
	"Chromium": true,
	"Edge": true,
	"Firefox": true,
	"Safari": true,
}

func userAgentTester(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := user_agent.New(r.UserAgent())
		browser, _ := ua.Browser()
		if !SupportedBrowsers[browser] {
			fmt.Fprintf(w, "Your browser is not supported! Please use a modern browser such as Chrome, Firefox, Edge or safari.")
			return
		}
		h.ServeHTTP(w, r)
	})
}

func makeHTTPServer() *http.Server {
	mux := &http.ServeMux{}
	mux.HandleFunc("/ws", webSocketHandler)
	mux.Handle("/", userAgentTester(gziphandler.GzipHandler(http.FileServer(FS(*useLocalAsset)))))
	return makeServerFromMux(mux)
}

func makeRedirectServer() *http.Server {
	handleRedirect := func(w http.ResponseWriter, r *http.Request) {
		newURI := "https://" + r.Host + r.URL.String()
		http.Redirect(w, r, newURI, http.StatusFound)
	}
	mux := &http.ServeMux{}
	mux.HandleFunc("/", handleRedirect)
	return makeServerFromMux(mux)
}

func main() {
	flag.Parse()
	patches := make(map[string]string)
	if len(*customStyleFile) > 0 {
		customStyle, err := ioutil.ReadFile(*customStyleFile)
		if err != nil {
			log.Fatal(err)
		}
		patches["<style custom=\"true\"></style>"] = fmt.Sprintf("<style>%s</style>", customStyle)
	}
	if len(patches) > 0 {
		err := PatchFiles(*regexp.MustCompile("index.html"), patches)
		if err != nil {
			log.Fatal(err)
		}
	}
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		os.RemoveAll("/tmp/paguridae")
		os.Exit(0)
	}()

	// Initialize HTTP(s) servers
	var m *autocert.Manager
	if *useHttps {
		hostPolicy := func(ctx context.Context, host string) error {
			allowedHost := *domain
			if host == allowedHost {
				return nil
			}
			return fmt.Errorf("acme/autocert: only %s host is allowed", allowedHost)
		}
		m = &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: hostPolicy,
			Cache:      autocert.DirCache(*certCache),
		}

		httpsSrv := makeHTTPServer()
		httpsSrv.Addr = ":443"
		httpsSrv.TLSConfig = &tls.Config{GetCertificate: m.GetCertificate}

		go func() {
			log.Printf("Starting HTTPS server on %s", httpsSrv.Addr)
			log.Fatal(httpsSrv.ListenAndServeTLS("", ""))
		}()
	}
	if *useHttps && *port != 80 {
		log.Printf("Warning: HTTPS is started, but HTTP server is not started on port 80, please make sure this is what you want")
	}
	var httpSrv *http.Server
	if *useHttps && *redirectToHttps {
		httpSrv = makeRedirectServer()
	} else {
		httpSrv = makeHTTPServer()
	}
	// allow autocert handle Let's Encrypt callbacks over http
	if m != nil {
		httpSrv.Handler = m.HTTPHandler(httpSrv.Handler)
	}

	httpSrv.Addr = fmt.Sprintf(":%d", *port)
	log.Printf("Starting HTTP server on port: %d", *port)
	log.Fatal(httpSrv.ListenAndServe())
}
