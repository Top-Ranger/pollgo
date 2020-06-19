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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/Top-Ranger/pollgo/datasafe"
	"github.com/Top-Ranger/pollgo/registry"
)

// ConfigStruct contains all configuration options for PollGo!
type ConfigStruct struct {
	Language           string
	MaxNumberQuestions int
	Address            string
	PathImpressum      string
	PathDSGVO          string
	Passwords          []string
	DataSafe           string
	DataSafeConfig     string
	RunGCOnStart       bool
}

var config ConfigStruct
var safe registry.DataSafe

func loadConfig(path string) (ConfigStruct, error) {
	log.Printf("main: Loading config (%s)", path)
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return ConfigStruct{}, errors.New(fmt.Sprintln("Can not read config.json:", err))
	}

	c := ConfigStruct{}
	err = json.Unmarshal(b, &c)
	if err != nil {
		return ConfigStruct{}, errors.New(fmt.Sprintln("Error while parsing config.json:", err))
	}

	for i := range c.Passwords {
		c.Passwords[i] = encodePassword(c.Passwords[i])
	}

	return c, nil
}

func main() {
	configPath := flag.String("config", "./config.json", "Path to json config for QuestionGo!")
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

	datasafe, ok := registry.GetDataSafe(config.DataSafe)
	if !ok {
		log.Panicf("main: Unknown data safe %s", config.DataSafe)
	}

	b, err := ioutil.ReadFile(config.DataSafeConfig)
	if err != nil {
		log.Panicln(err)
	}

	err = datasafe.LoadConfig(b)
	if err != nil {
		log.Panicln(err)
	}

	safe = datasafe

	if config.RunGCOnStart {
		safe.RunGC()
	}

	RunServer()

	s := make(chan os.Signal)
	signal.Notify(s, os.Interrupt, syscall.SIGTERM)

	log.Println("main: waiting")

	for range s {
		StopServer()
		datasafe.FlushAndClose()
		return
	}
}
