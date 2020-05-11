package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io/ioutil"
	"regexp"
)

// This is a hack into esc, hence I'm maintaining it as a separate file,
// it's more likely we need to adjust the code here when newer version of esc
// comes out.
func PatchFiles(files regexp.Regexp, patches map[string]string) error {
	for file, f := range _escData {
		if files.Match([]byte(file)) {
			var gr *gzip.Reader
			b64 := base64.NewDecoder(base64.StdEncoding, bytes.NewBufferString(f.compressed))
			gr, err := gzip.NewReader(b64)
			if err != nil {
				return err
			}
			content, err := ioutil.ReadAll(gr)
			if err != nil {
				return err
			}
			for original, replaced := range patches {
				content = bytes.ReplaceAll(content, []byte(original), []byte(replaced))
			}
			var buf bytes.Buffer
			gw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
			if err != nil {
				return err
			}
			if _, err := gw.Write(content); err != nil {
				return err
			}
			if err := gw.Close(); err != nil {
				return err
			}
			var b bytes.Buffer
			b64encoder := base64.NewEncoder(base64.StdEncoding, &b)
			b64encoder.Write(buf.Bytes())
			b64encoder.Close()
			f.size = int64(len(content))
			f.compressed = b.String()
		}
	}
	return nil
}
