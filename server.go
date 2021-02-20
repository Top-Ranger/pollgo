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
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var serverMutex sync.Mutex
var serverStarted bool
var server http.Server
var rootPath string

//go:embed template
var templateFiles embed.FS
var textTemplate *template.Template

var dsgvo []byte
var impressum []byte

//go:embed static font js css
var cachedFiles embed.FS
var etagCompare string
var cssTemplates *template.Template

var robottxt = []byte(`User-agent: *
Disallow: /`)

func init() {
	var err error
	textTemplate, err = template.ParseFS(templateFiles, "template/text.html")
	if err != nil {
		panic(err)
	}

	cssTemplates, err = template.ParseFS(cachedFiles, "css/*")
	if err != nil {
		panic(err)
	}
}

const startpage = `
<h1>PollGo!</h1>

<script>
function toRandomPage() {
  var b = new Uint8Array(33);
  window.crypto.getRandomValues(b);
  var target = window.location.href;
  if(target.slice(-1) != "/") {
    target = target + "/";
  }
  var id = btoa(String.fromCharCode.apply(null, b));
  id = id.replace(new RegExp("/", "g"), "-")
  target = target + id
  window.location.href = target;
}
</script>

<div id="__randompoll" hidden>
<button onclick="toRandomPage()">%s</button>
</div>

<script>var e = document.getElementById("__randompoll"); e.removeAttribute("hidden");</script>

<div class="even">
<h2>%s:</h2>
<noscript>%s</noscript>
<ul class="starlist" id="starlist">
</ul>
</div>

<script>
try {
  let a = getPolls();
  let t = document.getElementById("starlist");
  let keys = Object.keys(a);
  let c = new Intl.Collator();
  keys.sort(function(k, l){
	if(a[k].Display) {
		k = a[k].Display;
	}
	if(a[l].Display) {
		l = a[l].Display;
	}
	return c.compare(k, l);
  });
  for(let i = 0; i < keys.length; i++) {
	let link = document.createElement("A");
	link.href = "/" + keys[i];
	if(a[keys[i]].Display) {
		link.textContent = a[keys[i]].Display;
	} else {
		link.textContent = keys[i];
	}
	let li = document.createElement("LI");
	li.appendChild(link);
	t.appendChild(li);
  }
} catch (e) {
	console.log(e)
}
</script>
`

type textTemplateStruct struct {
	Text        template.HTML
	Translation Translation
	ServerPath  string
}

func initialiseServer() error {
	if serverStarted {
		return nil
	}
	server = http.Server{Addr: config.Address}

	// Do setup
	rootPath = strings.Join([]string{config.ServerPath, "/"}, "")

	// DSGVO
	b, err := ioutil.ReadFile(config.PathDSGVO)
	if err != nil {
		return err
	}
	text := textTemplateStruct{Format(b), GetDefaultTranslation(), config.ServerPath}
	output := bytes.NewBuffer(make([]byte, 0, len(text.Text)*2))
	textTemplate.Execute(output, text)
	dsgvo = output.Bytes()

	http.HandleFunc(strings.Join([]string{config.ServerPath, "/dsgvo.html"}, ""), func(rw http.ResponseWriter, r *http.Request) {
		rw.Write(dsgvo)
	})

	// Impressum
	b, err = ioutil.ReadFile(config.PathImpressum)
	if err != nil {
		return err
	}
	text = textTemplateStruct{Format(b), GetDefaultTranslation(), config.ServerPath}
	output = bytes.NewBuffer(make([]byte, 0, len(text.Text)*2))
	textTemplate.Execute(output, text)
	impressum = output.Bytes()
	http.HandleFunc(strings.Join([]string{config.ServerPath, "/impressum.html"}, ""), func(rw http.ResponseWriter, r *http.Request) {
		rw.Write(impressum)
	})

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
		path = strings.TrimPrefix(path, config.ServerPath)
		path = strings.TrimPrefix(path, "/")

		if strings.HasSuffix(path, ".css") {
			// special case
			rw.Header().Set("Content-Type", "text/css")
			err := cssTemplates.ExecuteTemplate(rw, path, struct{ ServerPath string }{config.ServerPath})
			if err != nil {
				log.Println("server:", err)
			}
			return
		}

		data, err := cachedFiles.Open(path)
		if err != nil {
			rw.WriteHeader(http.StatusNotFound)
		} else {
			rw.Header().Set("ETag", etag)
			rw.Header().Set("Cache-Control", "public, max-age=43200")
			switch {
			case strings.HasSuffix(path, ".svg"):
				rw.Header().Set("Content-Type", "image/svg+xml")
			case strings.HasSuffix(path, ".ttf"):
				rw.Header().Set("Content-Type", "application/x-font-truetype")
			case strings.HasSuffix(path, ".js"):
				rw.Header().Set("Content-Type", "application/javascript")
			default:
				rw.Header().Set("Content-Type", "text/plain")
			}
			io.Copy(rw, data)
		}
	}

	http.HandleFunc(strings.Join([]string{config.ServerPath, "/css/"}, ""), staticHandle)
	http.HandleFunc(strings.Join([]string{config.ServerPath, "/static/"}, ""), staticHandle)
	http.HandleFunc(strings.Join([]string{config.ServerPath, "/font/"}, ""), staticHandle)
	http.HandleFunc(strings.Join([]string{config.ServerPath, "/js/"}, ""), staticHandle)

	http.HandleFunc(strings.Join([]string{config.ServerPath, "/favicon.ico"}, ""), func(rw http.ResponseWriter, r *http.Request) {
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

		f, err := cachedFiles.ReadFile("static/favicon.ico")

		if err != nil {
			rw.WriteHeader(http.StatusNotFound)
			return
		}

		rw.Write(f)
	})

	// robots.txt
	http.HandleFunc(strings.Join([]string{config.ServerPath, "/robots.txt"}, ""), func(rw http.ResponseWriter, r *http.Request) {
		rw.Write(robottxt)
	})

	http.HandleFunc("/", rootHandle)
	return nil
}

func rootHandle(rw http.ResponseWriter, r *http.Request) {
	if r.URL.Path == rootPath || r.URL.Path == config.ServerPath || r.URL.Path == "/" {
		rw.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		tl := GetDefaultTranslation()
		text := fmt.Sprintf(startpage, template.HTMLEscapeString(tl.CreateNewPollRandom), template.HTMLEscapeString(tl.Starred), template.HTMLEscapeString(tl.FunctionRequiresJavaScript))
		t := textTemplateStruct{template.HTML(text), tl, config.ServerPath}
		textTemplate.Execute(rw, t)
		return
	}

	key := r.URL.Path
	key = strings.TrimLeft(key, "/")

	c, err := safe.GetPollConfig(key)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
		textTemplate.Execute(rw, t)
		return
	}

	p, err := LoadPoll(c)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
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
