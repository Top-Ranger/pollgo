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

package datasafe

import (
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Top-Ranger/pollgo/registry"
)

func init() {
	fm := new(fileMemory)
	fm.l = new(sync.Mutex)
	fm.flushandclose = make(chan bool, 1)
	fm.flushandclosereturn = make(chan bool, 1)
	fm.memory = make(map[string]FileMemoryPollResult)
	err := registry.RegisterDataSafe(fm, FileMemoryName)
	if err != nil {
		panic(err)
	}
}

// ErrNotActive is an error which is returned if fileMemory is used without initialising
var ErrNotActive = errors.New("filememory was not activated")

// ErrInvalidID is an error which is returned if ID is invalid
var ErrInvalidID = errors.New("filememory got invalid ID")

// FileMemoryName contains the name of the DataSafe
const FileMemoryName = "FileMemory"

type fileMemory struct {
	ClearInterval int
	MaximumMemory int
	Path          string

	memory              map[string]FileMemoryPollResult
	active              bool
	l                   *sync.Mutex
	flushandclose       chan bool
	flushandclosereturn chan bool
}

// FileMemoryPollResult is a helper struct which holds the Results of a poll
type FileMemoryPollResult struct {
	Data       [][]int
	Names      []string
	Comments   []string
	Config     []byte
	LastAccess time.Time
	Deleted    bool
}

func (fm fileMemory) getInternalID(ID string) (string, error) {
	// ﷐
	if strings.Contains(ID, "﷐") {
		return "", ErrInvalidID
	}
	return strings.ReplaceAll(ID, string(os.PathSeparator), "﷐"), nil
}

func (fm *fileMemory) SavePollResult(pollID, name, comment string, results []int) error {
	fm.l.Lock()
	defer fm.l.Unlock()
	if !fm.active {
		return ErrNotActive
	}
	err := fm.testload(pollID)
	if err != nil {
		return err
	}

	pollID, err = fm.getInternalID(pollID)
	if err != nil {
		return err
	}

	p := fm.memory[pollID]
	p.Data = append(p.Data, results)
	p.Names = append(p.Names, name)
	p.Comments = append(p.Comments, comment)
	p.LastAccess = time.Now()
	fm.memory[pollID] = p
	return nil
}

func (fm *fileMemory) GetPollResult(pollID string) ([][]int, []string, []string, error) {
	fm.l.Lock()
	defer fm.l.Unlock()
	if !fm.active {
		return nil, nil, nil, ErrNotActive
	}

	err := fm.testload(pollID)
	if err != nil {
		return nil, nil, nil, err
	}

	pollID, err = fm.getInternalID(pollID)
	if err != nil {
		return nil, nil, nil, err
	}

	p := fm.memory[pollID]
	p.LastAccess = time.Now()
	fm.memory[pollID] = p
	return p.Data, p.Names, p.Comments, nil
}

func (fm *fileMemory) SavePollConfig(pollID string, config []byte) error {
	fm.l.Lock()
	defer fm.l.Unlock()
	if !fm.active {
		return ErrNotActive
	}
	err := fm.testload(pollID)
	if err != nil {
		return err
	}

	pollID, err = fm.getInternalID(pollID)
	if err != nil {
		return err
	}

	p := fm.memory[pollID]
	p.Config = config
	p.LastAccess = time.Now()
	fm.memory[pollID] = p
	return nil
}

func (fm *fileMemory) GetPollConfig(pollID string) ([]byte, error) {
	fm.l.Lock()
	defer fm.l.Unlock()
	if !fm.active {
		return nil, ErrNotActive
	}

	err := fm.testload(pollID)
	if err != nil {
		return nil, err
	}

	pollID, err = fm.getInternalID(pollID)
	if err != nil {
		return nil, err
	}

	p := fm.memory[pollID]
	p.LastAccess = time.Now()
	fm.memory[pollID] = p
	return p.Config, nil
}

func (fm *fileMemory) MarkPollDeleted(pollID string) error {
	fm.l.Lock()
	defer fm.l.Unlock()
	if !fm.active {
		return ErrNotActive
	}
	err := fm.testload(pollID)
	if err != nil {
		return err
	}

	pollID, err = fm.getInternalID(pollID)
	if err != nil {
		return err
	}

	p := fm.memory[pollID]
	p.Deleted = true
	p.LastAccess = time.Now()
	fm.memory[pollID] = p
	return nil
}

func (fm *fileMemory) RunGC() error {
	fm.l.Lock()
	defer fm.l.Unlock()
	if !fm.active {
		return ErrNotActive
	}

	// First remove deleted entries from memory
	for k := range fm.memory {
		if fm.memory[k].Deleted {
			err := fm.save(k)
			if err != nil {
				return err
			}
			delete(fm.memory, k)
		}
	}

	// Test all files
	dir, err := os.Open(fm.Path)
	if err != nil {
		return err
	}
	defer dir.Close()

	files, err := dir.Readdir(-1)
	if err != nil {
		return err
	}

	for f := range files {
		if files[f].IsDir() || !files[f].Mode().IsRegular() {
			continue
		}
		fmpr, err := fm.load(files[f].Name())
		if err != nil {
			return err
		}
		// File is deleted if either it is marked as deleted or there was never a configuration written to it (e.g. never a poll created).
		// Second check is included for old PollGo versions
		if fmpr.Deleted || fmpr.Config == nil {
			// Delete file
			err := os.Remove(filepath.Join(fm.Path, files[f].Name()))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (fm *fileMemory) LoadConfig(data []byte) error {
	fm.l.Lock()
	defer fm.l.Unlock()
	if fm.active {
		return ErrNotActive
	}

	err := json.Unmarshal(data, fm)
	if err != nil {
		return err
	}
	if fm.MaximumMemory <= 0 {
		return errors.New("filememory: MaximumMemory must be positive")
	}
	if fm.ClearInterval <= 0 {
		return errors.New("filememory: ClearInterval must be positive")
	}

	err = os.MkdirAll(filepath.Join(fm.Path), os.ModePerm)
	if err != nil {
		return err
	}

	go fm.worker()
	fm.active = true
	return nil
}

func (fm *fileMemory) FlushAndClose() {
	fm.l.Lock()
	if !fm.active {
		fm.l.Unlock()
		return
	}
	fm.l.Unlock()

	// in case this was already called and channel is blocked
	select {
	case fm.flushandclose <- true:
	default:
	}

	// wait until result channel is closed
	for range fm.flushandclosereturn {
	}
}

type fileMemoryHelper struct {
	id string
	t  time.Time
}

type fileMemoryHelperArray []fileMemoryHelper

func (h fileMemoryHelperArray) Len() int {
	return len(h)
}

func (h fileMemoryHelperArray) Less(i, j int) bool {
	return h[i].t.Before(h[j].t)
}

func (h fileMemoryHelperArray) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (fm *fileMemory) worker() {
	fm.l.Lock()
	duration := time.Duration(fm.ClearInterval) * time.Minute
	fm.l.Unlock()
	t := time.NewTicker(duration)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			func() {
				fm.l.Lock()
				defer fm.l.Unlock()
				if len(fm.memory) <= fm.MaximumMemory {
					return
				}

				helper := make([]fileMemoryHelper, 0, len(fm.memory))

				for k := range fm.memory {
					helper = append(helper, fileMemoryHelper{id: k, t: fm.memory[k].LastAccess})
				}
				sort.Sort(fileMemoryHelperArray(helper))

				i := 0

				for len(fm.memory) > fm.MaximumMemory {
					err := fm.save(helper[i].id)
					if err != nil {
						log.Printf("filememory: error saving %s: %s", helper[i].id, err.Error())
					}
					delete(fm.memory, helper[i].id)
					i++
				}
				log.Printf("filememory: freed %d resources from memory", i)
			}()
		case <-fm.flushandclose:
			func() {
				fm.l.Lock()
				defer fm.l.Unlock()
				for k := range fm.memory {
					err := fm.save(k)
					if err != nil {
						log.Printf("filememory: error saving %s: %s", k, err.Error())
					}
				}
				fm.memory = make(map[string]FileMemoryPollResult, 0)
				fm.active = false
			}()
			close(fm.flushandclosereturn)
			return
		}
	}
}

// Internal functions

// caller has to lock
func (fm *fileMemory) testload(pollID string) error {
	pollID, err := fm.getInternalID(pollID)
	if err != nil {
		return err
	}

	_, ok := fm.memory[pollID]
	if ok {
		// already loaded
		return nil
	}

	fmpr, err := fm.load(pollID)
	if err != nil {
		return err
	}

	fm.memory[pollID] = fmpr
	return nil
}

func (fm *fileMemory) load(ID string) (FileMemoryPollResult, error) {
	f, err := os.Open(filepath.Join(fm.Path, ID))
	defer f.Close()
	if os.IsNotExist(err) {
		// No data was ever saved, just create an empty result
		return FileMemoryPollResult{LastAccess: time.Now()}, nil
	} else if err != nil {
		// some file error
		return FileMemoryPollResult{LastAccess: time.Now()}, err
	}
	dec := gob.NewDecoder(f)
	var data [][]int
	var names []string
	var comments []string
	var config []byte
	var deleted bool
	err = dec.Decode(&data)
	if err != nil && err != io.EOF {
		return FileMemoryPollResult{LastAccess: time.Now()}, err
	}
	err = dec.Decode(&names)
	if err != nil && err != io.EOF {
		return FileMemoryPollResult{LastAccess: time.Now()}, err
	}
	err = dec.Decode(&comments)
	if err != nil && err != io.EOF {
		return FileMemoryPollResult{LastAccess: time.Now()}, err
	}
	err = dec.Decode(&config)
	if err != nil && err != io.EOF {
		return FileMemoryPollResult{LastAccess: time.Now()}, err
	}
	err = dec.Decode(&deleted)
	if err != nil && err != io.EOF {
		return FileMemoryPollResult{LastAccess: time.Now()}, err
	}
	fmpr := FileMemoryPollResult{
		Data:       data,
		Names:      names,
		Comments:   comments,
		Config:     config,
		LastAccess: time.Now(),
		Deleted:    deleted,
	}
	return fmpr, nil
}

func (fm *fileMemory) save(ID string) error {
	p, ok := fm.memory[ID]
	if !ok {
		return fmt.Errorf("filememory: can not find %s", ID)
	}

	// Don't save polls with no configuration
	if p.Config == nil {
		return nil
	}

	// Save poll
	f, err := os.Create(filepath.Join(fm.Path, ID))
	defer f.Close()
	if err != nil {
		// some file error
		return err
	}
	enc := gob.NewEncoder(f)
	err = enc.Encode(&p.Data)
	if err != nil {
		return err
	}
	err = enc.Encode(&p.Names)
	if err != nil {
		return err
	}
	err = enc.Encode(&p.Comments)
	if err != nil {
		return err
	}
	err = enc.Encode(&p.Config)
	if err != nil {
		return err
	}
	err = enc.Encode(&p.Deleted)
	if err != nil {
		return err
	}
	return nil
}
