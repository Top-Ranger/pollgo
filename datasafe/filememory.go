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
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Top-Ranger/pollgo/registry"
)

func init() {
	fm := new(FileMemory)
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

// FileMemory holds a number of polls in memory and saves all other to disk.
type FileMemory struct {
	// Interval in minutes when a cleanup operation is started.
	// A cleanup operation will reduce memory if MaximumMemory is exceeded by saving polls to disk.
	ClearInterval int

	// Ratio of 'free' memory versus used memory.
	// Example: if set to 0.75 and Maximum memory is set to 100, then 75 polls will be kept in memory after cleanup.
	ClearAfterRatio float64

	// Number of polls needed in memory before cleanup is performed.
	MaximumMemory int

	// Interval in minutes in which all polls in memory will be synced to disk.
	// This is used to reduce damage if something goes horribly wrong.
	// Setting this to 0 disables syncing to disk.
	DiscSyncInterval int

	//  Path where polls are saved to disk.
	Path string

	memory              map[string]FileMemoryPollResult
	active              bool
	l                   *sync.Mutex
	flushandclose       chan bool
	flushandclosereturn chan bool
}

// FileMemoryPollResult is a helper struct which holds the Results of a poll.
// The data is only guaranteed to be saved to disk after FlushAndClose is called.
type FileMemoryPollResult struct {
	Data       [][]int
	Names      []string
	Comments   []string
	Config     []byte
	LastAccess time.Time
	Deleted    bool
	Creator    string
}

func (fm FileMemory) getInternalID(ID string) (string, error) {
	// ﷐
	if strings.Contains(ID, "﷐") {
		return "", ErrInvalidID
	}
	return strings.ReplaceAll(ID, string(os.PathSeparator), "﷐"), nil
}

// SavePollResult saves the results of a single poll.
func (fm *FileMemory) SavePollResult(pollID, name, comment string, results []int) error {
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

// GetPollResult returns the results of a poll.
func (fm *FileMemory) GetPollResult(pollID string) ([][]int, []string, []string, error) {
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

// SavePollConfig saves the poll configuration.
func (fm *FileMemory) SavePollConfig(pollID string, config []byte) error {
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

// GetPollConfig returns the poll configuration.
func (fm *FileMemory) GetPollConfig(pollID string) ([]byte, error) {
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

// SavePollCreator sets the poll creator.
func (fm *FileMemory) SavePollCreator(pollID, name string) error {
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
	p.Creator = name
	p.LastAccess = time.Now()
	fm.memory[pollID] = p
	return nil
}

// GetPollCreator returns the poll creator.
func (fm *FileMemory) GetPollCreator(pollID string) (string, error) {
	fm.l.Lock()
	defer fm.l.Unlock()
	if !fm.active {
		return "", ErrNotActive
	}

	err := fm.testload(pollID)
	if err != nil {
		return "", err
	}

	pollID, err = fm.getInternalID(pollID)
	if err != nil {
		return "", err
	}

	p := fm.memory[pollID]
	p.LastAccess = time.Now()
	fm.memory[pollID] = p
	return p.Creator, nil

}

// MarkPollDeleted marks a poll as deleted. It is not deleted imidiately, but on next garbage collect.
func (fm *FileMemory) MarkPollDeleted(pollID string) error {
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

// RunGC runs the garbage collection and removes deleted polls.
func (fm *FileMemory) RunGC() error {
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

	deleted := 0

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
			deleted++
		}
	}

	log.Printf("filememory: gc removed %d resources from disc", deleted)

	return nil
}

// LoadConfig loads the configuration of the FileMemory from JSON encoded data.
func (fm *FileMemory) LoadConfig(data []byte) error {
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
	if fm.DiscSyncInterval < 0 {
		return errors.New("filememory: ClearInterval must be positive or zero")
	}

	if fm.ClearAfterRatio < 0.0 || fm.ClearAfterRatio > 1.0 {
		return errors.New("filememory: ClearAfterRatio must be between 0.0 and 1.0")
	}

	if fm.ClearAfterRatio < 0.5 {
		log.Printf("filememory: ClearAfterRatio is low - most polls will be removed on cleanup")
	}

	err = os.MkdirAll(filepath.Join(fm.Path), os.ModePerm)
	if err != nil {
		return err
	}

	go fm.worker()
	fm.active = true
	return nil
}

// FlushAndClose saves all poll to disk.
// It is only guarateed that the data is saved to disk if this function is called.
func (fm *FileMemory) FlushAndClose() {
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

// Internal

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

func (fm *FileMemory) worker() {
	fm.l.Lock()
	durationClear := time.Duration(fm.ClearInterval) * time.Minute
	durationSync := time.Duration(fm.DiscSyncInterval) * time.Minute
	fm.l.Unlock()
	clear := time.NewTicker(durationClear)
	defer clear.Stop()
	var sync time.Ticker
	if durationSync != 0 {
		sync = *time.NewTicker(durationSync)
		defer sync.Stop()
	}
	for {
		select {
		case <-clear.C:
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

				target := int(math.Ceil(float64(fm.MaximumMemory) * fm.ClearAfterRatio))

				for len(fm.memory) > target {
					err := fm.save(helper[i].id)
					if err != nil {
						log.Printf("filememory: error saving %s: %s", helper[i].id, err.Error())
					}
					delete(fm.memory, helper[i].id)
					i++
				}
				log.Printf("filememory: freed %d resources from memory", i)
			}()
		case <-sync.C:
			func() {
				fm.l.Lock()
				defer fm.l.Unlock()

				for k := range fm.memory {
					fm.save(k)
				}
				log.Printf("filememory: synced %d resources to disc", len(fm.memory))
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

// caller has to lock
func (fm *FileMemory) testload(pollID string) error {
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

func (fm *FileMemory) load(ID string) (FileMemoryPollResult, error) {
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
	var creator string
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
	err = dec.Decode(&creator)
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
		Creator:    creator,
	}
	return fmpr, nil
}

func (fm *FileMemory) save(ID string) error {
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
	err = enc.Encode(&p.Creator)
	if err != nil {
		return err
	}
	return nil
}
