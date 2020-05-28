package main

import (
	"bytes"
	"crypto/sha256"
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
		matches := regexp.MustCompile(`<link href="([^"]+)"(?: hash="([^"]+)")? rel="stylesheet">`).FindSubmatchIndex(content)
		if len(matches) != 6 {
			break
		}
		var hash *string
		if matches[4] != -1 && matches[5] != -1 {
			h := string(content[matches[4]:matches[5]])
			hash = &h
		}
		cssContent, err := resolveContent(string(content[matches[2]:matches[3]]), hash)
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
		matches := regexp.MustCompile(`<script src="(?:[^"]+)" production="([^"]+)"(?: hash="([^"]+)")?.*><\/script>`).FindSubmatchIndex(content)
		if len(matches) != 6 {
			break
		}
		var hash *string
		if matches[4] != -1 && matches[5] != -1 {
			h := string(content[matches[4]:matches[5]])
			hash = &h
		}
		jsContent, err := resolveContent(string(content[matches[2]:matches[3]]), hash)
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

func resolveContent(uri string, expectedHash *string) ([]byte, error) {
	if strings.HasPrefix(uri, "http") {
		// Remote file
		resp, err := http.Get(uri)
		if err != nil {
			return nil, err
		}
		content, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		actualHash := fmt.Sprintf("%x", sha256.Sum256(content))
		if expectedHash != nil {
			if actualHash != *expectedHash {
				return nil, fmt.Errorf("Hash mismatch for URI %s! Expected: %s, actual: %s", uri, *expectedHash, actualHash)
			}
		} else {
			log.Printf("Fetching content from uri %s, hash: %s", uri, actualHash)
		}
		return content, nil
	} else {
		// Local file
		path := filepath.Clean(fmt.Sprintf("%s/../%s", *inputFile, uri))
		return ioutil.ReadFile(path)
	}
}
