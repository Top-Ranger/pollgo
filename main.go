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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	_ "github.com/Top-Ranger/pollgo/authenticater"
	_ "github.com/Top-Ranger/pollgo/datasafe"
	"github.com/Top-Ranger/pollgo/registry"
)

// ConfigStruct contains all configuration options for PollGo!
type ConfigStruct struct {
	Language              string
	MaxNumberQuestions    int
	Address               string
	PathImpressum         string
	PathDSGVO             string
	AuthenticationEnabled bool
	Authenticater         string
	AuthenticaterConfig   string
	LogFailedLogin        bool
	OnlyCreatorCanDelete  bool
	DataSafe              string
	DataSafeConfig        string
	RunGCOnStart          bool
	ServerPath            string
	EditCookieDays        int
}

var config ConfigStruct
var safe registry.DataSafe
var authenticater registry.Authenticater

func loadConfig(path string) (ConfigStruct, error) {
	log.Printf("main: Loading config (%s)", path)
	b, err := os.ReadFile(path)
	if err != nil {
		return ConfigStruct{}, errors.New(fmt.Sprintln("Can not read config.json:", err))
	}

	c := ConfigStruct{}
	err = json.Unmarshal(b, &c)
	if err != nil {
		return ConfigStruct{}, errors.New(fmt.Sprintln("Error while parsing config.json:", err))
	}

	if !strings.HasPrefix(c.ServerPath, "/") && c.ServerPath != "" {
		log.Println("load config: ServerPath does not start with '/', adding it as a prefix")
		c.ServerPath = strings.Join([]string{"/", c.ServerPath}, "")
	}
	c.ServerPath = strings.TrimSuffix(c.ServerPath, "/")

	if !c.AuthenticationEnabled && c.OnlyCreatorCanDelete {
		log.Println("load config: Configuration nonsensical - OnlyCreatorCanDelete has no effect when AuthenticationEnabled is false")
	}

	return c, nil
}

func printInfo() {
	log.Println("PollGo!")
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		log.Print("- no build info found")
		return
	}

	log.Printf("- go version: %s", bi.GoVersion)
	for _, s := range bi.Settings {
		switch s.Key {
		case "-tags":
			log.Printf("- build tags: %s", s.Value)
		case "vcs.revision":
			l := 7
			if len(s.Value) > 7 {
				s.Value = s.Value[:l]
			}
			log.Printf("- commit: %s", s.Value)
		case "vcs.modified":
			log.Printf("- files modified: %s", s.Value)
		}
	}
}

func main() {
	configPath := flag.String("config", "./config.json", "Path to json config for PollGo!")
	flag.Parse()

	c, err := loadConfig(*configPath)
	if err != nil {
		panic(err)
	}
	config = c

	err = SetDefaultTranslation(config.Language)
	if err != nil {
		log.Panicf("main: Error setting default language '%s': %s", config.Language, err.Error())
	}
	log.Printf("main: Setting language to '%s'", config.Language)

	{
		datasafe, ok := registry.GetDataSafe(config.DataSafe)
		if !ok {
			log.Panicf("main: Unknown data safe %s", config.DataSafe)
		}

		b, err := os.ReadFile(config.DataSafeConfig)
		if err != nil {
			log.Panicln(err)
		}

		err = datasafe.LoadConfig(b)
		if err != nil {
			log.Panicln(err)
		}

		safe = datasafe
	}

	if config.AuthenticationEnabled {
		a, ok := registry.GetAuthenticater(config.Authenticater)
		if !ok {
			log.Panicf("main: Unknown authenticater %s", config.Authenticater)
		}

		b, err := os.ReadFile(config.AuthenticaterConfig)
		if err != nil {
			log.Panicln(err)
		}

		err = a.LoadConfig(b)
		if err != nil {
			log.Panicln(err)
		}

		authenticater = a

	}

	if config.RunGCOnStart {
		log.Println("main: starting gc")
		safe.RunGC()
		log.Println("main: gc finished")
	}

	RunServer()

	s := make(chan os.Signal, 1)
	signal.Notify(s, os.Interrupt, syscall.SIGTERM)

	log.Println("main: waiting")

	for range s {
		StopServer()
		safe.FlushAndClose()
		return
	}
}
