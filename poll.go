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
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/colors"
)

// Poll represents a single poll.
// All methods are not save for concurrent use.
// It is adviced to create an own instance for each concurrent use.
// Results will be shared throuh the DataSafe.
type Poll struct {
	AnswerOption [][]string // [text, value, colour]
	Questions    []string
	Description  string
	Deleted      bool
	initialised  bool
}

type pollTemplateStruct struct {
	Key             string
	Questions       []string
	Answers         [][][]string // [][Question][text, colour]
	AnswerWhiteFont [][]bool
	Names           []string
	Comments        []string
	Points          []float64
	BestValue       float64
	Description     template.HTML
	HasPassword     bool
	Translation     Translation
}

type answerTemplateStruct struct {
	Key          string
	AnswerOption [][]string // [text, value, colour]
	Questions    []string
	Description  template.HTML
	Translation  Translation
}

type newTemplateStruct struct {
	Key         string
	HasPassword bool
	Translation Translation
}

var pollTemplate *template.Template
var answerTemplate *template.Template
var newTemplate *template.Template

var deleteTemplate = template.Must(template.New("poll").Parse(`
<script>
try {
	var a = JSON.parse(localStorage.getItem("pollgo_star"));
	var i = a.indexOf("{{.}}");
	if(i != -1) {
		a.splice(i, 1)
		localStorage.setItem("pollgo_star", JSON.stringify(a));
	}
} catch (e) {
}
</script>
`))

func init() {
	b, err := ioutil.ReadFile("template/poll.html")
	if err != nil {
		panic(err)
	}

	pollTemplate, err = template.New("poll").Parse(string(b))
	if err != nil {
		panic(err)
	}

	b, err = ioutil.ReadFile("template/answer.html")
	if err != nil {
		panic(err)
	}

	answerTemplate, err = template.New("answer").Parse(string(b))
	if err != nil {
		panic(err)
	}

	b, err = ioutil.ReadFile("template/new.html")
	if err != nil {
		panic(err)
	}

	newTemplate, err = template.New("new").Parse(string(b))
	if err != nil {
		panic(err)
	}
}

func sanitiseKey(key string) string {
	return template.HTMLEscapeString(key)
}

// VerifyPollConfig will verify whether the configuration of the poll is valid.
func VerifyPollConfig(p Poll) bool {
	if len(p.AnswerOption) == 0 {
		return false
	}

	for i := range p.AnswerOption {
		if len(p.AnswerOption[i]) != 3 {
			return false
		}
		if _, err := strconv.ParseFloat(p.AnswerOption[i][1], 64); err != nil {
			return false
		}
		if _, err := colors.ParseHEX(p.AnswerOption[i][2]); err != nil {
			return false
		}
	}

	if len(p.Questions) == 0 {
		return false
	}

	return true
}

// LoadPoll loads  and initialises the poll from the current provided configuration.
// PLEASE NOTE: The loaded poll is not verified. If you use an untrusted source, you need to verify the poll else the behaviour is undefined.
func LoadPoll(config []byte) (Poll, error) {
	if len(config) == 0 {
		return Poll{initialised: false}, nil
	}
	var p Poll
	err := json.Unmarshal(config, &p)
	if err != nil {
		return Poll{initialised: false}, err
	}
	p.initialised = true
	return p, nil
}

// ExportPoll returns the configuration of the poll at the time of calling.
// The configuration is human readable.
func (p Poll) ExportPoll() ([]byte, error) {
	b, err := json.Marshal(&p)
	return b, err
}

// HandleRequest handles a web request to this poll. The key needs to be provided.
func (p *Poll) HandleRequest(rw http.ResponseWriter, r *http.Request, key string) {
	rw.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	switch r.Method {
	case http.MethodPost:
		if p.initialised {
			// This is an existing poll
			err := r.ParseForm()
			if err != nil {
				rw.WriteHeader(http.StatusInternalServerError)
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation()}
				textTemplate.Execute(rw, t)
				return
			}

			if r.Form.Get("delete") == "true" {
				// Delete this poll and return

				// Test password first
				if len(config.Passwords) != 0 {
					pw := encodePassword(r.Form.Get("pw"))
					correct := false
					for i := range config.Passwords {
						if pw == config.Passwords[i] {
							correct = true
							break
						}
					}
					if !correct {
						rw.WriteHeader(http.StatusForbidden)
						t := textTemplateStruct{"403 Forbidden", GetDefaultTranslation()}
						textTemplate.Execute(rw, t)
						return
					}
				}

				p.Deleted = true
				b, err := p.ExportPoll()
				if err != nil {
					rw.WriteHeader(http.StatusInternalServerError)
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation()}
					textTemplate.Execute(rw, t)
					return
				}
				err = safe.SavePollConfig(key, b)
				if err != nil {
					rw.WriteHeader(http.StatusInternalServerError)
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation()}
					textTemplate.Execute(rw, t)
					return
				}
				err = safe.MarkPollDeleted(key)
				if err != nil {
					rw.WriteHeader(http.StatusInternalServerError)
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation()}
					textTemplate.Execute(rw, t)
					return
				}
				http.Redirect(rw, r, fmt.Sprintf("/%s", key), http.StatusSeeOther)
				return
			}

			if r.Form.Get("exportConfig") == "true" {
				b, err := p.ExportPoll()
				if err != nil {
					rw.WriteHeader(http.StatusInternalServerError)
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation()}
					textTemplate.Execute(rw, t)
					return
				}
				rw.Write(b)
				return
			}

			// Test DSGVO first
			if r.Form.Get("dsgvo") == "" {
				rw.WriteHeader(http.StatusForbidden)
				t := textTemplateStruct{"403 Forbidden", GetDefaultTranslation()}
				textTemplate.Execute(rw, t)
				return
			}

			results := make([]int, len(p.Questions))
			for i := range p.Questions {
				a := r.Form.Get(strconv.Itoa(i))
				ai, err := strconv.Atoi(a)
				if err != nil {
					rw.WriteHeader(http.StatusBadRequest)
					t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation()}
					textTemplate.Execute(rw, t)
					return
				}
				if ai >= len(p.AnswerOption) {
					rw.WriteHeader(http.StatusBadRequest)
					t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation()}
					textTemplate.Execute(rw, t)
					return
				}
				results[i] = ai
			}
			err = safe.SavePollResult(key, r.Form.Get("name"), r.Form.Get("comment"), results)
			if err != nil {
				rw.WriteHeader(http.StatusInternalServerError)
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation()}
				textTemplate.Execute(rw, t)
				return
			}
			http.Redirect(rw, r, fmt.Sprintf("/%s", key), http.StatusSeeOther)
			return
		}
		// This is a new poll
		if p.initialised {
			rw.WriteHeader(http.StatusBadRequest)
			t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation()}
			textTemplate.Execute(rw, t)
			return
		}

		err := r.ParseForm()
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation()}
			textTemplate.Execute(rw, t)
			return
		}
		// Test password first
		if len(config.Passwords) != 0 {
			pw := encodePassword(r.Form.Get("pw"))
			correct := false
			for i := range config.Passwords {
				if pw == config.Passwords[i] {
					correct = true
					break
				}
			}
			if !correct {
				rw.WriteHeader(http.StatusForbidden)
				t := textTemplateStruct{"403 Forbidden", GetDefaultTranslation()}
				textTemplate.Execute(rw, t)
				return
			}
		}
		// Test DSGVO first
		if r.Form.Get("dsgvo") == "" {
			rw.WriteHeader(http.StatusForbidden)
			t := textTemplateStruct{"403 Forbidden", GetDefaultTranslation()}
			textTemplate.Execute(rw, t)
			return
		}

		p.AnswerOption = make([][]string, 0)
		p.Questions = make([]string, 0)

		switch r.Form.Get("type") {
		case "normal":
			p.Description = r.Form.Get("description")
			// Questions
			searchid := 0
			budget := config.MaxNumberQuestions
			for {
				searchid++
				name := r.Form.Get(fmt.Sprintf("normalanswer%d", searchid))
				if name == "" {
					break
				}
				p.Questions = append(p.Questions, name)
				budget--
				if budget < 0 {
					rw.WriteHeader(http.StatusBadRequest)
					tl := GetDefaultTranslation()
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(tl.PollToLargeError)), tl}
					textTemplate.Execute(rw, t)
					return
				}
			}
			// Answers
			searchid = 0
			budget = config.MaxNumberQuestions
			for {
				searchid++
				answer := r.Form.Get(fmt.Sprintf("normalansweroption%d", searchid))
				if answer == "" {
					break
				}
				value := r.Form.Get(fmt.Sprintf("normalanswervalue%d", searchid))
				if value == "" {
					value = "0.0"
				} else if _, err := strconv.ParseFloat(value, 64); err != nil {
					value = "0.0"
				}
				colour := r.Form.Get(fmt.Sprintf("normalanswercolour%d", searchid))
				if colour == "" {
					colour = "#ffffff"
				}

				p.AnswerOption = append(p.AnswerOption, []string{answer, value, colour})
				budget--
				if budget < 0 {
					rw.WriteHeader(http.StatusBadRequest)
					tl := GetDefaultTranslation()
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(tl.PollToLargeError)), tl}
					textTemplate.Execute(rw, t)
					return
				}
			}
			if len(p.Questions) == 0 || len(p.AnswerOption) == 0 {
				rw.WriteHeader(http.StatusBadRequest)
				tl := GetDefaultTranslation()
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(tl.PollNoOptions)), tl}
				textTemplate.Execute(rw, t)
				return
			}
			if !VerifyPollConfig(*p) {
				rw.WriteHeader(http.StatusBadRequest)
				t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation()}
				textTemplate.Execute(rw, t)
				return
			}
			p.initialised = true
		case "date":
			t := GetDefaultTranslation()
			p.AnswerOption = [][]string{{t.DateYes, "1.0", "#5EFF5E"}, {t.DateOnlyIfNeeded, "0.25", "#FFE75E"}, {t.DateNo, "-1.0", "#FF5E66"}, {t.DateCanNotSay, "0.0", "#DBD9E2"}}
			var dateRead = "2006-01-02"
			var timeWrite = "02.01.2006 15:04"
			var timeWriteNoTime = "02.01.2006"

			p.Description = r.Form.Get("description")
			start, err := time.Parse(dateRead, r.Form.Get("start"))
			if err != nil {
				rw.WriteHeader(http.StatusInternalServerError)
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation()}
				textTemplate.Execute(rw, t)
				return
			}
			end, err := time.Parse(dateRead, r.Form.Get("end"))
			if err != nil {
				rw.WriteHeader(http.StatusInternalServerError)
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation()}
				textTemplate.Execute(rw, t)
				return
			}
			end = end.AddDate(0, 0, 1)
			weekdayMap := make(map[time.Weekday]bool, 7)
			if r.Form.Get("mo") != "" {
				weekdayMap[time.Monday] = true
			}
			if r.Form.Get("tu") != "" {
				weekdayMap[time.Tuesday] = true
			}
			if r.Form.Get("we") != "" {
				weekdayMap[time.Wednesday] = true
			}
			if r.Form.Get("th") != "" {
				weekdayMap[time.Thursday] = true
			}
			if r.Form.Get("fr") != "" {
				weekdayMap[time.Friday] = true
			}
			if r.Form.Get("sa") != "" {
				weekdayMap[time.Saturday] = true
			}
			if r.Form.Get("su") != "" {
				weekdayMap[time.Sunday] = true
			}
			times := make([][]int, 0)
			test := make(map[string]bool)
			searchid := 0
			for {
				searchid++
				name := r.Form.Get(fmt.Sprintf("time%d", searchid))
				if name == "" {
					break
				}
				tn := make([]int, 2)
				split := strings.Split(name, ":")
				if len(split) != 2 {
					break
				}
				tn[0], err = strconv.Atoi(split[0])
				if err != nil {
					rw.WriteHeader(http.StatusBadRequest)
					t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation()}
					textTemplate.Execute(rw, t)
					return
				}
				tn[1], err = strconv.Atoi(split[1])
				if err != nil {
					rw.WriteHeader(http.StatusBadRequest)
					t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation()}
					textTemplate.Execute(rw, t)
					return
				}

				if tn[0] < 0 || tn[0] > 23 {
					rw.WriteHeader(http.StatusBadRequest)
					t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation()}
					textTemplate.Execute(rw, t)
					return
				}

				if tn[1] < 0 || tn[1] > 59 {
					rw.WriteHeader(http.StatusBadRequest)
					t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation()}
					textTemplate.Execute(rw, t)
					return
				}

				// Ensure time format is identical
				timeTest := fmt.Sprintf("%d:%d", tn[0], tn[1])
				if test[timeTest] {
					continue
				}
				test[timeTest] = true

				times = append(times, tn)
			}

			sort.Sort(timesSort(times))

			// Generate questions
			budget := config.MaxNumberQuestions
			for start.Before(end) {
				process := start
				start = start.AddDate(0, 0, 1)
				if !weekdayMap[process.Weekday()] {
					continue
				}
				if r.Form.Get("notime") != "" {
					p.Questions = append(p.Questions, FormatTimeDisplay(process, timeWriteNoTime))
				}

				for i := range times {
					p.Questions = append(p.Questions, FormatTimeDisplay(time.Date(process.Year(), process.Month(), process.Day(), times[i][0], times[i][1], 0, 0, process.Location()), timeWrite))
				}
				budget--
				if budget < 0 {
					rw.WriteHeader(http.StatusBadRequest)
					tl := GetDefaultTranslation()
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(tl.PollToLargeError)), tl}
					textTemplate.Execute(rw, t)
					return
				}
			}
			if len(p.Questions) == 0 || len(p.AnswerOption) == 0 {
				rw.WriteHeader(http.StatusBadRequest)
				tl := GetDefaultTranslation()
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(tl.PollNoOptions)), tl}
				textTemplate.Execute(rw, t)
				return
			}
			if !VerifyPollConfig(*p) {
				rw.WriteHeader(http.StatusBadRequest)
				t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation()}
				textTemplate.Execute(rw, t)
				return
			}
			p.initialised = true
		case "config":
			c := r.Form.Get("config")
			if c == "" {
				rw.WriteHeader(http.StatusBadRequest)
				t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation()}
				textTemplate.Execute(rw, t)
				return
			}
			new, err := LoadPoll([]byte(c))
			if err != nil {
				rw.WriteHeader(http.StatusBadRequest)
				t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation()}
				textTemplate.Execute(rw, t)
				return
			}
			if !VerifyPollConfig(new) {
				rw.WriteHeader(http.StatusBadRequest)
				t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation()}
				textTemplate.Execute(rw, t)
				return
			}
			p.AnswerOption = new.AnswerOption
			p.Questions = new.Questions
			p.Description = new.Description
			p.Deleted = false
			p.initialised = true
		default:
			rw.WriteHeader(http.StatusBadRequest)
			t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation()}
			textTemplate.Execute(rw, t)
			return
		}
		b, err := p.ExportPoll()
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation()}
			textTemplate.Execute(rw, t)
			return
		}
		err = safe.SavePollConfig(key, b)
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation()}
			textTemplate.Execute(rw, t)
			return
		}
		http.Redirect(rw, r, fmt.Sprintf("/%s", key), http.StatusSeeOther)
		return
	case http.MethodGet:
		// Test if this is deleted
		if p.Deleted {
			rw.WriteHeader(http.StatusGone)
			tl := GetDefaultTranslation()
			buf := bytes.Buffer{}
			deleteTemplate.Execute(&buf, key)
			text := strings.Join([]string{template.HTMLEscapeString(tl.PollIsDeleted), buf.String()}, "\n")
			t := textTemplateStruct{template.HTML(text), tl}
			textTemplate.Execute(rw, t)
			return
		}

		if p.initialised {
			// This is an existing poll
			err := r.ParseForm()
			if err != nil {
				rw.WriteHeader(http.StatusInternalServerError)
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation()}
				textTemplate.Execute(rw, t)
				return
			}
			a := r.Form.Get("answer")
			if a != "" {
				// Answer requested
				td := answerTemplateStruct{
					Key:          sanitiseKey(key),
					AnswerOption: p.AnswerOption,
					Questions:    p.Questions,
					Description:  Format([]byte(p.Description)),
					Translation:  GetDefaultTranslation(),
				}
				err = answerTemplate.Execute(rw, td)
				if err != nil {
					log.Printf("Poll.HandleRequest.answer: %s", err.Error())
				}
				return
			}

			// Poll requested
			r, n, c, err := safe.GetPollResult(key)
			if err != nil {
				rw.WriteHeader(http.StatusInternalServerError)
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation()}
				textTemplate.Execute(rw, t)
				return
			}

			// Verify data
			if len(r) != len(n) {
				rw.WriteHeader(http.StatusInternalServerError)
				log.Printf("Poll.HandleRequest (%s):  len(r) != len(n)", key)
				t := textTemplateStruct{"len(r) != len(n)", GetDefaultTranslation()}
				textTemplate.Execute(rw, t)
				return
			}

			for i := range r {
				if len(r[i]) != len(p.Questions) {
					rw.WriteHeader(http.StatusInternalServerError)
					log.Printf("Poll.HandleRequest (%s):  len(r[%d]) != len(p.Questions)", key, i)
					t := textTemplateStruct{"len(r[i]) != len(p.Questions)", GetDefaultTranslation()}
					textTemplate.Execute(rw, t)
					return
				}
			}

			td := pollTemplateStruct{
				Key:             sanitiseKey(key),
				Questions:       p.Questions,
				Answers:         make([][][]string, len(n)),
				AnswerWhiteFont: make([][]bool, len(n)),
				Names:           n,
				Comments:        c,
				Points:          make([]float64, len(p.Questions)),
				BestValue:       math.Inf(-1),
				Description:     Format([]byte(p.Description)),
				HasPassword:     len(config.Passwords) != 0,
				Translation:     GetDefaultTranslation(),
			}

			for i := range r {
				answer := make([][]string, len(p.Questions))
				whitefont := make([]bool, len(p.Questions))
				for a := range r[i] {
					if r[i][a] < len(p.AnswerOption) {
						answer[a] = []string{p.AnswerOption[r[i][a]][0], p.AnswerOption[r[i][a]][2]}
						f, err := strconv.ParseFloat(p.AnswerOption[r[i][a]][1], 64)
						if err != nil {
							f = 0.0
							log.Printf("Poll.HandleRequest (%s): strconv.ParseFloat(p.AnswerOption[r[%d][%d]][1], 64) %s", key, i, a, err.Error())
						}
						td.Points[a] += f
						col, err := colors.ParseHEX(p.AnswerOption[r[i][a]][2])
						if err == nil {
							whitefont[a] = col.IsDark()
						}
					} else {
						// Something is wrong
						log.Printf("Poll.HandleRequest (%s):  r[%d][%d] < len(p.AnswerOption)", key, i, a)
						answer[a] = []string{"error", "#ffffff"}
					}
				}
				td.Answers[i] = answer
				td.AnswerWhiteFont[i] = whitefont
			}

			for i := range td.Points {
				td.BestValue = math.Max(td.BestValue, td.Points[i])
			}

			err = pollTemplate.Execute(rw, td)
			if err != nil {
				log.Printf("Poll.HandleRequest.poll: %s", err.Error())
			}
			return
		}
		// This is a new poll
		td := newTemplateStruct{
			Key:         sanitiseKey(key),
			HasPassword: len(config.Passwords) != 0,
			Translation: GetDefaultTranslation(),
		}
		err := newTemplate.Execute(rw, td)
		if err != nil {
			log.Printf("Poll.HandleRequest.new: %s", err.Error())
		}
		return
	}
}

type timesSort [][]int

func (t timesSort) Len() int {
	return len(t)
}

func (t timesSort) Less(i, j int) bool {
	if t[i][0] < t[j][0] {
		return true
	}
	if t[i][0] > t[j][0] {
		return false
	}
	return t[i][1] < t[j][1]
}

func (t timesSort) Swap(i, j int) {
	t[i], t[j] = t[j], t[i]
}
