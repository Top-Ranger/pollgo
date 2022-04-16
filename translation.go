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
	"embed"
	"encoding/json"
	"log"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

// Translation represents an object holding all translations
type Translation struct {
	Language                   string
	WeekdayMonday              string
	WeekdayTuesday             string
	WeekdayWednesday           string
	WeekdayThursday            string
	WeekdayFriday              string
	WeekdaySaturday            string
	WeekdaySunday              string
	DateYes                    string
	DateNo                     string
	DateOnlyIfNeeded           string
	DateCanNotSay              string
	Name                       string
	Optional                   string
	Points                     string
	Submit                     string
	CreatedBy                  string
	Impressum                  string
	PrivacyPolicy              string
	NewPoll                    string
	NormalPoll                 string
	AppointmentPoll            string
	Question                   string
	AnswerOption               string
	Value                      string
	Colour                     string
	Description                string
	AddOption                  string
	Yes                        string
	No                         string
	Username                   string
	Password                   string
	AcceptPrivacyPolicy        string
	CreatePoll                 string
	Time                       string
	StartDate                  string
	EndDate                    string
	NoTime                     string
	AddTime                    string
	Participate                string
	SelectPollKind             string
	Results                    string
	PollToLargeError           string
	PollNoOptions              string
	DeletePoll                 string
	PollIsDeleted              string
	Starred                    string
	LoadConfiguration          string
	Configuration              string
	MoreOptions                string
	ExportConfiguration        string
	Comment                    string
	Unknown                    string
	SelectAll                  string
	FunctionRequiresJavaScript string
	UserNotCreator             string
	CreateNewPollRandom        string
	PleaseWait                 string
	AuthentificationFailure    string
	ErrorOccured               string
	OpinionPoll                string
	OpinionItem                string
	AddOpinionItem             string
	OpinionGood                string
	OpinionRatherGood          string
	OpinionNeutral             string
	OpinionRatherBad           string
	OpinionBad                 string
	InvalidKey                 string
	EditAnswer                 string
	DeleteAnswer               string
	RememberedAs               string
}

const defaultLanguage = "en"

//go:embed translation
var translationFiles embed.FS

var initialiseCurrent sync.Once
var current Translation
var rwlock sync.RWMutex
var translationPath = "./translation"

// GetTranslation returns a Translation struct of the given language.
// This function always loads translations from disk. Try to use GetDefaultTranslation where possible.
func GetTranslation(language string) (Translation, error) {
	t, err := getSingleTranslation(language)
	if err != nil {
		return Translation{}, err
	}
	d, err := getSingleTranslation(defaultLanguage)
	if err != nil {
		return Translation{}, err
	}

	// Set unknown strings to default value
	vp := reflect.ValueOf(&t)
	dv := reflect.ValueOf(d)
	v := vp.Elem()

	for i := 0; i < v.NumField(); i++ {
		if !v.Field(i).CanSet() {
			continue
		}
		if v.Field(i).Kind() != reflect.String {
			continue
		}
		if v.Field(i).String() == "" {
			v.Field(i).SetString(dv.Field(i).String())
		}
	}
	return t, nil
}

func getSingleTranslation(language string) (Translation, error) {
	if language == "" {
		return GetDefaultTranslation(), nil
	}

	file := strings.Join([]string{language, "json"}, ".")
	file = filepath.Join(translationPath, file)

	b, err := translationFiles.ReadFile(file)
	if err != nil {
		return Translation{}, err
	}
	t := Translation{}
	err = json.Unmarshal(b, &t)
	if err != nil {
		return Translation{}, err
	}
	return t, nil
}

// SetDefaultTranslation sets the default language to the provided one.
// Does nothing if it returns error != nil.
func SetDefaultTranslation(language string) error {
	if language == "" {
		return nil
	}

	t, err := GetTranslation(language)
	rwlock.Lock()
	defer rwlock.Unlock()
	if err != nil {
		return err
	}
	current = t
	return nil
}

// GetDefaultTranslation returns a Translation struct of the current default language.
func GetDefaultTranslation() Translation {
	initialiseCurrent.Do(func() {
		rwlock.RLock()
		c := current.Language
		rwlock.RUnlock()
		if c == "" {
			err := SetDefaultTranslation(defaultLanguage)
			if err != nil {
				log.Printf("Can not load default language (%s): %s", defaultLanguage, err.Error())
			}
		}
	})
	rwlock.RLock()
	defer rwlock.RUnlock()
	return current
}
