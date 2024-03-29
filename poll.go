// SPDX-License-Identifier: Apache-2.0
// Copyright 2020,2022 Marcus Soll
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
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Top-Ranger/pollgo/helper"
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
	IDs             []string
	CanEdit         []bool
	Points          []float64
	BestValue       float64
	Description     template.HTML
	HasPassword     bool
	Translation     Translation
	ServerPath      string
}

type answerTemplateStruct struct {
	Key          string
	EditID       string
	AnswerOption [][]string // [text, value, colour]
	Questions    []string
	Description  template.HTML
	Name         string
	Comment      string
	Answers      []int
	Translation  Translation
	ServerPath   string
}

type newTemplateStruct struct {
	Key         string
	HasPassword bool
	Translation Translation
	ServerPath  string
}

var pollTemplate *template.Template
var answerTemplate *template.Template
var newTemplate *template.Template

var deleteTemplate = template.Must(template.New("poll").Parse(`
<script>
try {
	var a = getPolls();
	if (a["{{.}}"]) {
		delete a["{{.}}"];
		savePolls(a);
    }
} catch (e) {
}
</script>
`))

func init() {
	var err error
	pollTemplate, err = template.ParseFS(templateFiles, "template/poll.html")
	if err != nil {
		panic(err)
	}

	answerTemplate, err = template.ParseFS(templateFiles, "template/answer.html")
	if err != nil {
		panic(err)
	}

	newTemplate, err = template.ParseFS(templateFiles, "template/new.html")
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
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}

			if r.Form.Get("delete") == "true" {
				// Delete this poll and return

				// Test password first
				if config.AuthenticationEnabled {
					user, pw := r.Form.Get("user"), r.Form.Get("pw")
					if len(user) == 0 || len(pw) == 0 {
						rw.WriteHeader(http.StatusForbidden)
						t := textTemplateStruct{"403 Forbidden", GetDefaultTranslation(), config.ServerPath}
						textTemplate.Execute(rw, t)
						return
					}
					correct, err := authenticater.Authenticate(user, pw)
					if err != nil {
						rw.WriteHeader(http.StatusInternalServerError)
						t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
						textTemplate.Execute(rw, t)
						return
					}
					if !correct {
						if config.LogFailedLogin {
							log.Printf("Failed authentication from %s", GetRealIP(r))
						}
						rw.WriteHeader(http.StatusForbidden)
						t := textTemplateStruct{"403 Forbidden", GetDefaultTranslation(), config.ServerPath}
						textTemplate.Execute(rw, t)
						return
					}
				}

				// Test if user is creator - this can be skipped if no authentification is enabled
				if config.AuthenticationEnabled && config.OnlyCreatorCanDelete {
					user := r.Form.Get("user") // is already authenticated
					creator, err := safe.GetPollCreator(key)
					if err != nil {
						rw.WriteHeader(http.StatusInternalServerError)
						t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
						textTemplate.Execute(rw, t)
						return
					}
					if creator != "" && user != creator { // Also allow if creator is not set (e.g. old poll or poll created without authentification)
						tr := GetDefaultTranslation()
						rw.WriteHeader(http.StatusForbidden)
						t := textTemplateStruct{template.HTML(template.HTMLEscapeString(fmt.Sprintf("403 Forbidden (%s)", tr.UserNotCreator))), tr, config.ServerPath}
						textTemplate.Execute(rw, t)
						return
					}
				}

				p.Deleted = true
				b, err := p.ExportPoll()
				if err != nil {
					rw.WriteHeader(http.StatusInternalServerError)
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
				err = safe.SavePollConfig(key, b)
				if err != nil {
					rw.WriteHeader(http.StatusInternalServerError)
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
				err = safe.MarkPollDeleted(key)
				if err != nil {
					rw.WriteHeader(http.StatusInternalServerError)
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
				err = safe.SavePollCreator(key, "") // We don't need the creator any longer
				if err != nil {
					rw.WriteHeader(http.StatusInternalServerError)
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
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
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
				rw.Write(b)
				return
			}

			// Test if we should delete an answer
			if r.Form.Get("deleteAnswer") == "true" {
				// Delete answer
				answerID := r.Form.Get("answerID")

				change, err := safe.GetChange(key, answerID)
				if err != nil {
					rw.WriteHeader(http.StatusInternalServerError)
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
				if change == "" {
					rw.WriteHeader(http.StatusForbidden)
					t := textTemplateStruct{"403 Forbidden", GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
				cookies := r.Cookies()
				found := false
				for i := range cookies {
					if cookies[i].Name == answerID {
						if subtle.ConstantTimeCompare([]byte(change), []byte(cookies[i].Value)) == 0 {
							if config.LogFailedLogin {
								log.Printf("Failed authentication from %s", GetRealIP(r))
							}
							rw.WriteHeader(http.StatusForbidden)
							t := textTemplateStruct{"403 Forbidden", GetDefaultTranslation(), config.ServerPath}
							textTemplate.Execute(rw, t)
							return
						}
						found = true
					}
				}

				if !found {
					rw.WriteHeader(http.StatusForbidden)
					t := textTemplateStruct{"403 Forbidden", GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}

				err = safe.DeleteAnswer(key, answerID)
				if err != nil {
					rw.WriteHeader(http.StatusInternalServerError)
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}

				// Remove cookie
				cookie := http.Cookie{}
				cookie.Name = answerID
				cookie.Value = ""
				cookie.MaxAge = -1
				cookie.Path = fmt.Sprintf("/%s", key)
				cookie.SameSite = http.SameSiteLaxMode
				cookie.HttpOnly = true
				cookie.Secure = !config.InsecureAllowCookiesOverHTTP
				http.SetCookie(rw, &cookie)

				http.Redirect(rw, r, fmt.Sprintf("/%s", key), http.StatusSeeOther)

				return
			}

			// Test DSGVO first
			if r.Form.Get("dsgvo") == "" {
				rw.WriteHeader(http.StatusForbidden)
				t := textTemplateStruct{"403 Forbidden", GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}

			results := make([]int, len(p.Questions))
			for i := range p.Questions {
				a := r.Form.Get(strconv.Itoa(i))
				ai, err := strconv.Atoi(a)
				if err != nil {
					rw.WriteHeader(http.StatusBadRequest)
					t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
				if ai >= len(p.AnswerOption) {
					rw.WriteHeader(http.StatusBadRequest)
					t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
				results[i] = ai
			}
			change := helper.GetRandomString()

			answerID := r.Form.Get("answerID")
			if answerID == "" {
				answerID, err = safe.SavePollResult(key, r.Form.Get("name"), r.Form.Get("comment"), results, change)
				if err != nil {
					rw.WriteHeader(http.StatusInternalServerError)
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
			} else {
				change, err = safe.GetChange(key, answerID)
				if err != nil {
					rw.WriteHeader(http.StatusInternalServerError)
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
				if change == "" {
					rw.WriteHeader(http.StatusForbidden)
					t := textTemplateStruct{"403 Forbidden", GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
				cookies := r.Cookies()
				found := false
				for i := range cookies {
					if cookies[i].Name == answerID {
						if subtle.ConstantTimeCompare([]byte(change), []byte(cookies[i].Value)) == 0 {
							if config.LogFailedLogin {
								log.Printf("Failed authentication from %s", GetRealIP(r))
							}
							rw.WriteHeader(http.StatusForbidden)
							t := textTemplateStruct{"403 Forbidden", GetDefaultTranslation(), config.ServerPath}
							textTemplate.Execute(rw, t)
							return
						}
						found = true
					}
				}

				if !found {
					rw.WriteHeader(http.StatusForbidden)
					t := textTemplateStruct{"403 Forbidden", GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}

				err := safe.OverwritePollResult(key, answerID, r.Form.Get("name"), r.Form.Get("comment"), results, change)
				if err != nil {
					rw.WriteHeader(http.StatusInternalServerError)
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
			}

			// Set cookie for editing
			cookie := http.Cookie{}
			cookie.Name = answerID
			cookie.Value = change
			cookie.MaxAge = 24 * 60 * 60 * config.EditCookieDays
			cookie.Path = fmt.Sprintf("/%s", key)
			cookie.SameSite = http.SameSiteLaxMode
			cookie.HttpOnly = true
			cookie.Secure = !config.InsecureAllowCookiesOverHTTP
			http.SetCookie(rw, &cookie)

			http.Redirect(rw, r, fmt.Sprintf("/%s", key), http.StatusSeeOther)
			return
		}
		// This is a new poll
		if p.initialised {
			rw.WriteHeader(http.StatusBadRequest)
			t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation(), config.ServerPath}
			textTemplate.Execute(rw, t)
			return
		}

		err := r.ParseForm()
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
			textTemplate.Execute(rw, t)
			return
		}
		// Test password first
		if config.AuthenticationEnabled {
			user, pw := r.Form.Get("user"), r.Form.Get("pw")
			if len(user) == 0 || len(pw) == 0 {
				rw.WriteHeader(http.StatusForbidden)
				t := textTemplateStruct{"403 Forbidden", GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			correct, err := authenticater.Authenticate(user, pw)
			if err != nil {
				rw.WriteHeader(http.StatusInternalServerError)
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			if !correct {
				if config.LogFailedLogin {
					log.Printf("Failed authentication from %s", GetRealIP(r))
				}
				rw.WriteHeader(http.StatusForbidden)
				t := textTemplateStruct{"403 Forbidden", GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
		}
		// Test DSGVO first
		if r.Form.Get("dsgvo") == "" {
			rw.WriteHeader(http.StatusForbidden)
			t := textTemplateStruct{"403 Forbidden", GetDefaultTranslation(), config.ServerPath}
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
			searchuntil, err := strconv.Atoi(r.Form.Get("normalanswer"))
			if err != nil {
				rw.WriteHeader(http.StatusBadRequest)
				tl := GetDefaultTranslation()
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), tl, config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			budget := config.MaxNumberQuestions
			if searchuntil > budget*2 { // Allow for a few blank fields here
				rw.WriteHeader(http.StatusBadRequest)
				tl := GetDefaultTranslation()
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(tl.PollToLargeError)), tl, config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			for {
				searchid++
				if searchid > searchuntil+1 {
					break
				}
				name := r.Form.Get(fmt.Sprintf("normalanswer%d", searchid))
				if name == "" {
					continue
				}
				p.Questions = append(p.Questions, name)
				budget--
				if budget < 0 {
					rw.WriteHeader(http.StatusBadRequest)
					tl := GetDefaultTranslation()
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(tl.PollToLargeError)), tl, config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
			}
			// Answers
			searchid = 0
			searchuntil, err = strconv.Atoi(r.Form.Get("normalansweroption"))
			if err != nil {
				rw.WriteHeader(http.StatusBadRequest)
				tl := GetDefaultTranslation()
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), tl, config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			budget = config.MaxNumberQuestions
			if searchuntil > budget*2 { // Allow for a few blank fields here
				rw.WriteHeader(http.StatusBadRequest)
				tl := GetDefaultTranslation()
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(tl.PollToLargeError)), tl, config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			for {
				searchid++
				if searchid > searchuntil+1 {
					break
				}
				answer := r.Form.Get(fmt.Sprintf("normalansweroption%d", searchid))
				if answer == "" {
					continue
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
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(tl.PollToLargeError)), tl, config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
			}
			if len(p.Questions) == 0 || len(p.AnswerOption) == 0 {
				rw.WriteHeader(http.StatusBadRequest)
				tl := GetDefaultTranslation()
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(tl.PollNoOptions)), tl, config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			if !VerifyPollConfig(*p) {
				rw.WriteHeader(http.StatusBadRequest)
				t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			p.initialised = true
		case "date":
			t := GetDefaultTranslation()
			p.AnswerOption = [][]string{{t.DateYes, "1.0", "#243D00"}, {t.DateOnlyIfNeeded, "0.25", "#9A9A9A"}, {t.DateNo, "-1.0", "#E3C2D4"}, {t.DateCanNotSay, "0.0", "#F7F7F7"}}
			var dateRead = "2006-01-02"
			var timeWrite = "02.01.2006 15:04"
			var timeWriteNoTime = "02.01.2006"

			p.Description = r.Form.Get("description")
			start, err := time.Parse(dateRead, r.Form.Get("start"))
			if err != nil {
				rw.WriteHeader(http.StatusInternalServerError)
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			end, err := time.Parse(dateRead, r.Form.Get("end"))
			if err != nil {
				rw.WriteHeader(http.StatusInternalServerError)
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
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
			searchuntil, err := strconv.Atoi(r.Form.Get("timeanswer"))
			if err != nil {
				rw.WriteHeader(http.StatusBadRequest)
				tl := GetDefaultTranslation()
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), tl, config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			budget := config.MaxNumberQuestions
			if searchuntil > budget*2 { // Allow for a few blank fields here
				rw.WriteHeader(http.StatusBadRequest)
				tl := GetDefaultTranslation()
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(tl.PollToLargeError)), tl, config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			for {
				searchid++
				if searchid > searchuntil+1 {
					break
				}
				name := r.Form.Get(fmt.Sprintf("time%d", searchid))
				if name == "" {
					continue
				}
				tn := make([]int, 2)
				split := strings.Split(name, ":")
				if len(split) != 2 {
					break
				}
				tn[0], err = strconv.Atoi(split[0])
				if err != nil {
					rw.WriteHeader(http.StatusBadRequest)
					t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
				tn[1], err = strconv.Atoi(split[1])
				if err != nil {
					rw.WriteHeader(http.StatusBadRequest)
					t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}

				if tn[0] < 0 || tn[0] > 23 {
					rw.WriteHeader(http.StatusBadRequest)
					t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation(), config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}

				if tn[1] < 0 || tn[1] > 59 {
					rw.WriteHeader(http.StatusBadRequest)
					t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation(), config.ServerPath}
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
			budget = config.MaxNumberQuestions
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
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(tl.PollToLargeError)), tl, config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
			}
			if len(p.Questions) == 0 {
				rw.WriteHeader(http.StatusBadRequest)
				tl := GetDefaultTranslation()
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(tl.PollNoOptions)), tl, config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			if !VerifyPollConfig(*p) {
				rw.WriteHeader(http.StatusBadRequest)
				t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			p.initialised = true
		case "opinion":
			tl := GetDefaultTranslation()
			p.Description = r.Form.Get("description")
			// Questions
			searchid := 0
			searchuntil, err := strconv.Atoi(r.Form.Get("opinionitem"))
			if err != nil {
				rw.WriteHeader(http.StatusBadRequest)
				tl := GetDefaultTranslation()
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), tl, config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			budget := config.MaxNumberQuestions
			if searchuntil > budget*2 { // Allow for a few blank fields here
				rw.WriteHeader(http.StatusBadRequest)
				tl := GetDefaultTranslation()
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(tl.PollToLargeError)), tl, config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			for {
				searchid++
				if searchid > searchuntil+1 {
					break
				}
				name := r.Form.Get(fmt.Sprintf("opinionitem%d", searchid))
				if name == "" {
					continue
				}
				p.Questions = append(p.Questions, name)
				budget--
				if budget < 0 {
					rw.WriteHeader(http.StatusBadRequest)
					t := textTemplateStruct{template.HTML(template.HTMLEscapeString(tl.PollToLargeError)), tl, config.ServerPath}
					textTemplate.Execute(rw, t)
					return
				}
			}
			if len(p.Questions) == 0 {
				rw.WriteHeader(http.StatusBadRequest)
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(tl.PollNoOptions)), tl, config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}

			// Answers
			p.AnswerOption = [][]string{{tl.OpinionGood, "2", "#243D00"}, {tl.OpinionRatherGood, "1", "#5E842A"}, {tl.OpinionNeutral, "0", "#9A9A9A"}, {tl.OpinionRatherBad, "-1", "#E3C2D4"}, {tl.OpinionBad, "-2", "#FCFAFB"}}

			if !VerifyPollConfig(*p) {
				rw.WriteHeader(http.StatusBadRequest)
				t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			p.initialised = true
		case "config":
			c := r.Form.Get("config")
			if c == "" {
				rw.WriteHeader(http.StatusBadRequest)
				t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			new, err := LoadPoll([]byte(c))
			if err != nil {
				rw.WriteHeader(http.StatusBadRequest)
				t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			if !VerifyPollConfig(new) {
				rw.WriteHeader(http.StatusBadRequest)
				t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation(), config.ServerPath}
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
			t := textTemplateStruct{"400 Bad Request", GetDefaultTranslation(), config.ServerPath}
			textTemplate.Execute(rw, t)
			return
		}
		b, err := p.ExportPoll()
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
			textTemplate.Execute(rw, t)
			return
		}
		err = safe.SavePollConfig(key, b)
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
			textTemplate.Execute(rw, t)
			return
		}
		creator := ""
		if config.AuthenticationEnabled {
			creator = r.Form.Get("user") // is already authenticated
			err := safe.SavePollCreator(key, creator)
			if err != nil {
				rw.WriteHeader(http.StatusInternalServerError)
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
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
			t := textTemplateStruct{template.HTML(text), tl, config.ServerPath}
			textTemplate.Execute(rw, t)
			return
		}

		if p.initialised {
			// This is an existing poll
			err := r.ParseForm()
			if err != nil {
				rw.WriteHeader(http.StatusInternalServerError)
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}
			a := r.Form.Get("answer")
			if a != "" {
				// Answer requested
				td := answerTemplateStruct{
					Key:          sanitiseKey(key),
					EditID:       r.Form.Get("answerID"),
					AnswerOption: p.AnswerOption,
					Questions:    p.Questions,
					Description:  Format([]byte(p.Description)),
					Name:         "",
					Comment:      "",
					Answers:      nil,
					Translation:  GetDefaultTranslation(),
					ServerPath:   config.ServerPath,
				}

				if td.EditID != "" {
					r, n, c, err := safe.GetSinglePollResult(key, td.EditID)
					if err != nil {
						if err != nil {
							rw.WriteHeader(http.StatusInternalServerError)
							t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
							textTemplate.Execute(rw, t)
							return
						}
					}

					td.Name = n
					td.Comment = c
					td.Answers = r
				}

				for len(td.Answers) < len(p.Questions) {
					td.Answers = append(td.Answers, -1)
				}

				err = answerTemplate.Execute(rw, td)
				if err != nil {
					log.Printf("Poll.HandleRequest.answer: %s", err.Error())
				}
				return
			}

			// Poll requested
			cookies := r.Cookies()

			r, n, c, aid, err := safe.GetPollResult(key)
			if err != nil {
				rw.WriteHeader(http.StatusInternalServerError)
				t := textTemplateStruct{template.HTML(template.HTMLEscapeString(err.Error())), GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}

			// Verify data
			if len(r) != len(n) {
				rw.WriteHeader(http.StatusInternalServerError)
				log.Printf("Poll.HandleRequest (%s):  len(r) != len(n)", key)
				t := textTemplateStruct{"len(r) != len(n)", GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}

			if len(r) != len(c) {
				rw.WriteHeader(http.StatusInternalServerError)
				log.Printf("Poll.HandleRequest (%s):  len(r) != len(C)", key)
				t := textTemplateStruct{"len(r) != len(C)", GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}

			if len(r) != len(aid) {
				rw.WriteHeader(http.StatusInternalServerError)
				log.Printf("Poll.HandleRequest (%s):  len(r) != len(aid)", key)
				t := textTemplateStruct{"len(r) != len(aid)", GetDefaultTranslation(), config.ServerPath}
				textTemplate.Execute(rw, t)
				return
			}

			for i := range r {
				if len(r[i]) != len(p.Questions) {
					rw.WriteHeader(http.StatusInternalServerError)
					log.Printf("Poll.HandleRequest (%s):  len(r[%d]) != len(p.Questions)", key, i)
					t := textTemplateStruct{"len(r[i]) != len(p.Questions)", GetDefaultTranslation(), config.ServerPath}
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
				IDs:             aid,
				CanEdit:         make([]bool, len(n)),
				Points:          make([]float64, len(p.Questions)),
				BestValue:       math.Inf(-1),
				Description:     Format([]byte(p.Description)),
				HasPassword:     config.AuthenticationEnabled,
				Translation:     GetDefaultTranslation(),
				ServerPath:      config.ServerPath,
			}

			knownIDs := make(map[string]bool)
			for i := 0; i < len(cookies) && i < len(r)*2; i++ {
				knownIDs[cookies[i].Name] = true
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

				if knownIDs[aid[i]] {
					td.CanEdit[i] = true
				}
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
			HasPassword: config.AuthenticationEnabled,
			Translation: GetDefaultTranslation(),
			ServerPath:  config.ServerPath,
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
