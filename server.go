// SPDX-License-Identifier: Apache-2.0
// Copyright 2020 Marcus Soll
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var serverMutex sync.Mutex
var serverStarted bool
var server http.Server

var textTemplate *template.Template

var dsgvo []byte
var impressum []byte

var cachedFiles = make(map[string][]byte)
var etagCompare string

var robottxt = []byte(`User-agent: *
Disallow: /`)

func init() {
	b, err := ioutil.ReadFile("template/text.html")
	if err != nil {
		panic(err)
	}

	textTemplate, err = template.New("text").Parse(string(b))
	if err != nil {
		panic(err)
	}
}

const startpage = `
<h1>PollGo!</h1>

<div class="even">
<h2>%s:</h2>
<noscript>%s</noscript>
<ul class="starlist" id="starlist">
</ul>
</div>

<script>
try {
  var a = JSON.parse(localStorage.getItem("pollgo_star"));
  a.sort();
  var t = document.getElementById("starlist");
  for(var i = 0; i < a.length; i++) {
	var link = document.createElement("A");
	link.href = "/" + a[i];
	link.textContent = a[i];
	var li = document.createElement("LI");
	li.appendChild(link);
	t.appendChild(li);
  }
} catch (e) {
}
</script>
`

type textTemplateStruct struct {
	Text        template.HTML
	Translation Translation
}

func initialiseServer() error {
	if serverStarted {
		return nil
	}
	server = http.Server{Addr: config.Address}

	// Do setup
	// DSGVO
	b, err := ioutil.ReadFile(config.PathDSGVO)
	if err != nil {
		return err
	}
	text := textTemplateStruct{Format(b), GetDefaultTranslation()}
	output := bytes.NewBuffer(make([]byte, 0, len(text.Text)*2))
	textTemplate.Execute(output, text)
	dsgvo = output.Bytes()

	http.HandleFunc("/dsgvo.html", func(rw http.ResponseWriter, r *http.Request) {
		rw.Write(dsgvo)
	})

	// Impressum
	b, err = ioutil.ReadFile(config.PathImpressum)
	if err != nil {
		return err
	}
	text = textTemplateStruct{Format(b), GetDefaultTranslation()}
	output = bytes.NewBuffer(make([]byte, 0, len(text.Text)*2))
	textTemplate.Execute(output, text)
	impressum = output.Bytes()
	http.HandleFunc("/impressum.html", func(rw http.ResponseWriter, r *http.Request) {
		rw.Write(impressum)
	})

	// static files
	for _, d := range []string{"css/", "static/", "font/"} {
		filepath.Walk(d, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				log.Panicln("server: Error wile caching files:", err)
			}

			if info.Mode().IsRegular() {
				log.Println("static file handler: Caching file", path)

				b, err := ioutil.ReadFile(path)
				if err != nil {
					log.Println("static file handler: Error reading file:", err)
					return err
				}
				cachedFiles[path] = b
				return nil
			}
			return nil
		})
	}

	etag := fmt.Sprint("\"", strconv.FormatInt(time.Now().Unix(), 10), "\"")
	etagCompare := strings.TrimSuffix(etag, "\"")
	etagCompareApache := strings.Join([]string{etagCompare, "-"}, "")       // Dirty hack for apache2, who appends -gzip inside the quotes if the file is compressed, thus preventing If-None-Match matching the ETag
	etagCompareCaddy := strings.Join([]string{"W/", etagCompare, "\""}, "") // Dirty hack for caddy, who appends W/ before the quotes if the file is compressed, thus preventing If-None-Match matching the ETag

	staticHandle := func(rw http.ResponseWriter, r *http.Request) {
		// Check for ETag
		v, ok := r.Header["If-None-Match"]
		if ok {
			for i := range v {
				if v[i] == etag || v[i] == etagCompareCaddy || strings.HasPrefix(v[i], etagCompareApache) {
					rw.WriteHeader(http.StatusNotModified)
					return
				}
			}
		}

		// Send file if existing in cache
		path := r.URL.Path
		path = strings.TrimPrefix(path, "/")
		data, ok := cachedFiles[path]
		if !ok {
			rw.WriteHeader(http.StatusNotFound)
		} else {
			rw.Header().Set("ETag", etag)
			rw.Header().Set("Cache-Control", "public, max-age=43200")
			switch {
			case strings.HasSuffix(path, ".svg"):
				rw.Header().Set("Content-Type", "image/svg+xml")
			case strings.HasSuffix(path, ".css"):
				rw.Header().Set("Content-Type", "text/css")
			case strings.HasSuffix(path, ".ttf"):
				rw.Header().Set("Content-Type", "application/x-font-truetype")
			case strings.HasSuffix(path, ".js"):
				rw.Header().Set("Content-Type", "application/javascript")
			default:
				rw.Header().Set("Content-Type", "text/plain")
			}
			rw.Write(data)
		}
	}

	http.HandleFunc("/css/", staticHandle)
	http.HandleFunc("/static/", staticHandle)
	http.HandleFunc("/font/", staticHandle)
	http.HandleFunc("/js/", staticHandle)

	http.HandleFunc("/favicon.ico", func(rw http.ResponseWriter, r *http.Request) {
		// Check for ETag
		v, ok := r.Header["If-None-Match"]
		if ok {
			for i := range v {
				if v[i] == etag || v[i] == etagCompareCaddy || strings.HasPrefix(v[i], etagCompareApache) {
					rw.WriteHeader(http.StatusNotModified)
					return
				}
			}
		}

		f, ok := cachedFiles["static/favicon.ico"]

		if !ok {
			rw.WriteHeader(http.StatusNotFound)
			return
		}

		rw.Write(f)
	})

	// robots.txt
	http.HandleFunc("/robots.txt", func(rw http.ResponseWriter, r *http.Request) {
		rw.Write(robottxt)
	})

	http.HandleFunc("/", rootHandle)
	return nil
}

func rootHandle(rw http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		tl := GetDefaultTranslation()
		text := fmt.Sprintf(startpage, template.HTMLEscapeString(tl.Starred), template.HTMLEscapeString(tl.FunctionRequiresJavaScript))
		t := textTemplateStruct{template.HTML(text), tl}
		textTemplate.Execute(rw, t)
		return
	}

	key := r.URL.Path
	key = strings.TrimLeft(key, "/")

	c, err := safe.GetPollConfig(key)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation()}
		textTemplate.Execute(rw, t)
		return
	}

	p, err := LoadPoll(c)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation()}
		textTemplate.Execute(rw, t)
		return
	}
	p.HandleRequest(rw, r, key)
}

// RunServer starts the actual server.
// It does nothing if a server is already started.
// It will return directly after the server is started.
func RunServer() {
	serverMutex.Lock()
	defer serverMutex.Unlock()
	if serverStarted {
		return
	}

	err := initialiseServer()
	if err != nil {
		log.Panicln("server:", err)
	}
	log.Println("server: Server starting at", config.Address)
	serverStarted = true
	go func() {
		err = server.ListenAndServe()
		if err != http.ErrServerClosed {
			log.Println("server:", err)
		}
	}()
}

// StopServer shuts the server down.
// It will do nothing if the server is not started.
// It will return after the shutdown is completed.
func StopServer() {
	serverMutex.Lock()
	defer serverMutex.Unlock()
	if !serverStarted {
		return
	}
	err := server.Shutdown(context.Background())
	if err == nil {
		log.Println("server: stopped")
	} else {
		log.Println("server:", err)
	}
}
