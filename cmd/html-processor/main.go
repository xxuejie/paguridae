package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tdewolff/minify"
	"github.com/tdewolff/minify/css"
)

var inputFile = flag.String("inputFile", "", "Input file to use")
var outputFile = flag.String("outputFile", "-", "Output file to generate, use '-' to print to stdout")
var minimize = flag.Bool("minimize", true, "Minimize the files")

func main() {
	flag.Parse()

	content, err := ioutil.ReadFile(*inputFile)
	if err != nil {
		log.Fatal(err)
	}

	m := minify.New()
	m.AddFunc("text/css", css.Minify)

	for {
		matches := regexp.MustCompile(`<link href="([^"]+)" rel="stylesheet">`).FindSubmatchIndex(content)
		if len(matches) != 4 {
			break
		}
		cssContent, err := resolveContent(string(content[matches[2]:matches[3]]))
		if err != nil {
			log.Fatal(err)
		}
		if *minimize {
			var output bytes.Buffer
			input := bytes.NewReader(cssContent)
			err = m.Minify("text/css", &output, input)
			if err != nil {
				log.Fatal(err)
			}
			cssContent = output.Bytes()
		}
		fullCssContent := fmt.Sprintf("<style>%s</style>", cssContent)
		content = bytes.Join([][]byte{content[:matches[0]], content[matches[1]:]},
			[]byte(fullCssContent))
	}

	for {
		matches := regexp.MustCompile(`<script src="(?:[^"]+)" production="([^"]+)".*><\/script>`).FindSubmatchIndex(content)
		if len(matches) != 4 {
			break
		}
		jsContent, err := resolveContent(string(content[matches[2]:matches[3]]))
		if err != nil {
			log.Fatal(err)
		}
		if *minimize {
			jsContent = regexp.MustCompile(`//# sourceMappingURL=.*$`).ReplaceAll(jsContent, nil)
		}
		fullJsContent := fmt.Sprintf("<script>%s</script>", jsContent)
		content = bytes.Join([][]byte{content[:matches[0]], content[matches[1]:]},
			[]byte(fullJsContent))
	}

	writer := os.Stdout
	if *outputFile != "-" {
		writer, err = os.OpenFile(*outputFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			log.Fatal(err)
		}
	}

	_, err = writer.Write(content)
	if err != nil {
		log.Fatal(err)
	}
}

func resolveContent(uri string) ([]byte, error) {
	if strings.HasPrefix(uri, "http") {
		// Remote file
		resp, err := http.Get(uri)
		if err != nil {
			return nil, err
		}
		return ioutil.ReadAll(resp.Body)
	} else {
		// Local file
		path := filepath.Clean(fmt.Sprintf("%s/../%s", *inputFile, uri))
		return ioutil.ReadFile(path)
	}
}
