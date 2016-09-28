package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/russross/blackfriday"
)

func main() {
	var dest = ":8080"

	if len(os.Args) > 2 {
		log.Fatal("usage: md2html [dest]")
	}
	if len(os.Args) == 2 {
		dest = os.Args[1]
	}

	if !strings.Contains(dest, ":") {
		convert(dest)
		os.Exit(0)
	}

	serve(dest)
}

func serve(addr string) {
	fmt.Printf("serving at: http://localhost%s\n", addr)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := "." + r.URL.Path
		if filepath.Ext(path) != "" {
			http.ServeFile(w, r, path)
			return
		}
		b, err := render(path + ".md")
		if os.IsNotExist(err) {
			http.NotFound(w, r)
		} else if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Write(b)
	})
	log.Fatal(http.ListenAndServe(addr, nil))
}

func convert(dest string) {
	fmt.Printf("Converting markdown to: %s\n", dest)
	err := filepath.Walk(".", func(path string, f os.FileInfo, err error) error {
		if f.IsDir() {
			return nil
		}
		match, err := filepath.Match("*.md", f.Name())
		printe(err)
		if !match {
			b, err := ioutil.ReadFile(path)
			if err != nil {
				return err
			}
			err = os.MkdirAll(filepath.Dir(filepath.Join(dest, path)), 0777)
			if err != nil {
				return err
			}
			return ioutil.WriteFile(filepath.Join(dest, path), b, 0644)
		}
		b, err := render(path)
		printe(err)
		path = dest + "/" + path
		printe(os.MkdirAll(filepath.Dir(path), 0777))
		printe(ioutil.WriteFile(strings.TrimSuffix(path, ".md"), b, 0644))
		fmt.Printf("converted: %s\n", path)
		return nil
	})
	printe(err)
}

func printe(err error) {
	if err != nil {
		fmt.Println(err)
	}
}

func render(f string) ([]byte, error) {
	src, err := ioutil.ReadFile(f)
	if err != nil {
		return nil, err
	}

	layout, err := ioutil.ReadFile("layout.html")
	if os.IsNotExist(err) {
		layout = []byte("{{Body}}")
	} else if err != nil {
		return nil, err
	}

	return bytes.Replace(layout, []byte("{{Body}}"), markdown(src), 1), nil
}

func markdown(source []byte) []byte {
	htmlFlags := 0
	htmlFlags |= blackfriday.HTML_USE_XHTML
	htmlFlags |= blackfriday.HTML_USE_SMARTYPANTS
	htmlFlags |= blackfriday.HTML_SMARTYPANTS_FRACTIONS
	htmlFlags |= blackfriday.HTML_SMARTYPANTS_LATEX_DASHES
	htmlFlags |= blackfriday.HTML_SMARTYPANTS_DASHES
	htmlFlags |= blackfriday.HTML_FOOTNOTE_RETURN_LINKS
	renderer := blackfriday.HtmlRenderer(htmlFlags, "", "")

	extensions := 0
	extensions |= blackfriday.EXTENSION_FOOTNOTES
	extensions |= blackfriday.EXTENSION_NO_INTRA_EMPHASIS
	extensions |= blackfriday.EXTENSION_TABLES
	extensions |= blackfriday.EXTENSION_FENCED_CODE
	extensions |= blackfriday.EXTENSION_AUTOLINK
	extensions |= blackfriday.EXTENSION_STRIKETHROUGH
	extensions |= blackfriday.EXTENSION_SPACE_HEADERS
	extensions |= blackfriday.EXTENSION_HEADER_IDS
	extensions |= blackfriday.EXTENSION_LAX_HTML_BLOCKS
	extensions |= blackfriday.EXTENSION_AUTO_HEADER_IDS

	return blackfriday.Markdown(source, renderer, extensions)
}
