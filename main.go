package main

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/h2non/filetype"
	"github.com/rs/cors"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	staticDir = kingpin.Arg("directory", "Directory to serve").Required().String()
	noCache   = kingpin.Flag("no-cache", "Remove cache control").Bool()
)

func main() {
	kingpin.Version("0.0.1")
	kingpin.Parse()

	// Collect everything to map to make this just so fast
	// Since not using mutex remember to never write to this map again after init
	staticFiles := staticFilesToMap(*staticDir)

	log.Println("Found routes:")
	for key := range staticFiles {
		log.Println(key)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// Try to serve static file
			serveStatic(staticFiles, w, r)
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte("method not allowed"))
		}
	})
	c := cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"*"},
	})
	handler := c.Handler(mux)
	addr := ":8888"
	log.Printf("Listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, handler))
}

func sendNotFound(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("not found"))
}

type fileInMap struct {
	content     []byte
	contentType string
}

type files map[string]fileInMap

func staticFilesToMap(directory string) files {
	m := make(files)
	filepath.WalkDir(directory, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Fatal(err)
		}
		if !d.IsDir() {
			var contentType string
			splitted := strings.Split(path, ".")
			if len(splitted) != 0 {
				extension := splitted[len(splitted)-1]
				switch extension {
				case "html":
					contentType = "text/html"
				case "css":
					contentType = "text/css"
				case "txt":
					contentType = "text/plain"
				case "js":
					contentType = "text/javascript"
				case "md":
					contentType = "text/markdown"
				case "svg":
					contentType = "image/svg+xml"
				}
			}

			if contentType == "" {
				buf, err := ioutil.ReadFile(path)
				if err != nil {
					log.Fatal(err)
				}
				kind, err := filetype.Match(buf)
				if err != nil {
					log.Fatal(err)
				}
				if kind.MIME != filetype.Unknown.MIME {
					contentType = kind.MIME.Value
				} else {
					contentType = "text/plain"
				}
			}
			content, err := ioutil.ReadFile(path)
			if err != nil {
				log.Fatal(err)
			}
			formattedPath := strings.Replace(path, directory, "", 1)
			m[formattedPath] = fileInMap{
				content:     content,
				contentType: contentType,
			}
			// If index.html also navigate through dir root
			if strings.HasSuffix(formattedPath, "index.html") {
				rootPath := strings.TrimSuffix(formattedPath, "/index.html")
				m[rootPath] = fileInMap{
					content:     content,
					contentType: contentType,
				}
			}

		}
		return nil
	})
	return m
}

func serveStatic(f files, w http.ResponseWriter, r *http.Request) {
	sendStatic := func(value *fileInMap) {

		w.Header().Add("Content-Type", value.contentType)

		if !*noCache {
			if value.contentType != "text/html" &&
				value.contentType != "text/plain" &&
				value.contentType != "text/markdown" &&
				!strings.HasSuffix(r.URL.Path, "favicon.ico") {
				w.Header().Add("Etag", r.URL.Path)
				w.Header().Set("Cache-Control", "max-age=31536000")
			}

		}

		// Gzip data
		var b bytes.Buffer
		gz := gzip.NewWriter(&b)
		if _, err := gz.Write(value.content); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err := gz.Close(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		content := b.Bytes()
		w.Header().Add("Content-Encoding", "gzip")
		w.Header().Add("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}
	value, ok := f[r.URL.Path]
	if !ok {
		// Try with appending .html
		pathWithHtml := r.URL.Path + ".html"
		value, ok = f[pathWithHtml]
		if !ok {
			if strings.HasSuffix(r.URL.Path, "/") {
				// Try without slash
				formatted := strings.TrimSuffix(r.URL.Path, "/")
				value, ok = f[formatted]
				if !ok {
					sendNotFound(w)
				} else {
					sendStatic(&value)
				}
			} else {
				sendNotFound(w)
			}

		} else {
			sendStatic(&value)
		}
	} else {
		sendStatic(&value)
	}
}
