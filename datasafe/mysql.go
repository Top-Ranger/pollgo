//go:build mysql

package datasafe

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

import (
	"bytes"
	"database/sql"
	"encoding/gob"
	"errors"
	"fmt"
	"log"
	"strconv"

	_ "github.com/go-sql-driver/mysql"

	"github.com/Top-Ranger/pollgo/registry"
)

func init() {
	mysql := new(MySQL)
	err := registry.RegisterDataSafe(mysql, MySQLName)
	if err != nil {
		panic(err)
	}
}

// MySQLName contains the name of the DataSafe
const MySQLName = "MySQL"

// MySQLMaxLengthID is the maximum supported poll id length
const MySQLMaxLengthID = 500

// ErrMySQLUnknownID is returned when the requested poll is not in the database
var ErrMySQLIDtooLong = errors.New("mysql: id is too long")

// ErrMySQLUnknownID is returned when the requested poll is not in the database
var ErrMySQLUnknownID = errors.New("mysql: unknown poll id")

// ErrMySQLNotConfigured is returned when the database is used before it is configured
var ErrMySQLNotConfigured = errors.New("mysql: usage before configuration is used")

type MySQL struct {
	dsn string
	db  *sql.DB
}

func (m *MySQL) SavePollResult(pollID, name, comment string, results []int, change string) (string, error) {
	if m.db == nil {
		return "", ErrMySQLNotConfigured
	}

	if len(pollID) > MySQLMaxLengthID {
		return "", ErrMySQLIDtooLong
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(results)
	if err != nil {
		return "", fmt.Errorf("mysql: can not convert results: %w", err)
	}
	b := buf.Bytes()
	r, err := m.db.Exec("INSERT INTO result (poll, name, comment, results, `change`) VALUES (?,?,?,?,?)", pollID, name, comment, b, change)
	if err != nil {
		return "", err
	}
	lastInserted, err := r.LastInsertId()
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(lastInserted, 10), nil
}

func (m *MySQL) OverwritePollResult(pollID, answerID, name, comment string, results []int, change string) error {
	if m.db == nil {
		return ErrMySQLNotConfigured
	}

	if len(pollID) > MySQLMaxLengthID {
		return ErrMySQLIDtooLong
	}

	var id int64
	id, err := strconv.ParseInt(answerID, 10, 64)
	if err != nil {
		return fmt.Errorf("mysql: can not convert id '%s': %w", answerID, err)
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err = enc.Encode(results)
	if err != nil {
		return fmt.Errorf("mysql: can not convert results: %w", err)
	}
	b := buf.Bytes()
	_, err = m.db.Exec("UPDATE result SET name=?, comment=?, results=?, `change`=? WHERE poll=? AND id=?", name, comment, b, change, pollID, id)
	return err
}

func (m *MySQL) GetPollResult(pollID string) ([][]int, []string, []string, []string, error) {
	if m.db == nil {
		return nil, nil, nil, nil, ErrMySQLNotConfigured
	}

	if len(pollID) > MySQLMaxLengthID {
		return nil, nil, nil, nil, ErrMySQLIDtooLong
	}

	ids := make([]string, 0)
	results := make([][]int, 0)
	names := make([]string, 0)
	comments := make([]string, 0)

	rows, err := m.db.Query("SELECT id, name, comment, results FROM result WHERE poll=? ORDER BY id ASC", pollID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var r []byte
		var n, c string
		var id int64
		err = rows.Scan(&id, &n, &c, &r)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		buf := bytes.NewBuffer(r)
		dec := gob.NewDecoder(buf)
		var singleResult []int
		err := dec.Decode(&singleResult)
		if err != nil {
			log.Printf("mysql: can not decode results (ignoring it): %s", err.Error())
			continue
		}
		results = append(results, singleResult)
		names = append(names, n)
		comments = append(comments, c)
		ids = append(ids, strconv.FormatInt(id, 10))
	}

	return results, names, comments, ids, nil
}

func (m *MySQL) GetSinglePollResult(pollID, answerID string) ([]int, string, string, error) {
	if m.db == nil {
		return nil, "", "", ErrMySQLNotConfigured
	}

	if len(pollID) > MySQLMaxLengthID {
		return nil, "", "", ErrMySQLIDtooLong
	}

	var id int64
	id, err := strconv.ParseInt(answerID, 10, 64)
	if err != nil {
		return nil, "", "", fmt.Errorf("mysql: can not convert id '%s': %w", answerID, err)
	}

	rows, err := m.db.Query("SELECT name, comment, results FROM result WHERE poll=? AND id=?", pollID, id)
	if err != nil {
		return nil, "", "", err
	}
	defer rows.Close()

	if rows.Next() {
		var r []byte
		var n, c string
		err = rows.Scan(&n, &c, &r)
		if err != nil {
			return nil, "", "", err
		}
		buf := bytes.NewBuffer(r)
		dec := gob.NewDecoder(buf)
		var singleResult []int
		err := dec.Decode(&singleResult)
		if err != nil {
			return nil, "", "", fmt.Errorf("mysql: can not decode results: %w", err)
		}
		return singleResult, n, c, nil
	}

	return nil, "", "", ErrFileMemoryInvalidID
}

func (m *MySQL) SavePollConfig(pollID string, config []byte) error {
	if m.db == nil {
		return ErrMySQLNotConfigured
	}

	if len(pollID) > MySQLMaxLengthID {
		return ErrMySQLIDtooLong
	}

	_, err := m.db.Exec("INSERT INTO poll (name, data, deleted) VALUES (?,?,?) ON DUPLICATE KEY UPDATE data=?", pollID, config, false, config)

	return err
}

func (m *MySQL) GetPollConfig(pollID string) ([]byte, error) {
	if m.db == nil {
		return []byte{}, ErrMySQLNotConfigured
	}

	if len(pollID) > MySQLMaxLengthID {
		return []byte{}, ErrMySQLIDtooLong
	}

	r, err := m.db.Query("SELECT data FROM poll WHERE name=?", pollID)
	if err != nil {
		return []byte{}, err
	}
	defer r.Close()

	if !r.Next() {
		return []byte{}, nil
	}
	var data []byte
	err = r.Scan(&data)
	if err != nil {
		return []byte{}, err
	}
	return data, nil
}

func (m *MySQL) SavePollCreator(pollID, name string) error {
	if m.db == nil {
		return ErrMySQLNotConfigured
	}

	if len(pollID) > MySQLMaxLengthID {
		return ErrMySQLIDtooLong
	}

	_, err := m.db.Exec("UPDATE poll SET creator=? WHERE name=?", name, pollID)
	if err != nil {
		return err
	}

	return nil
}

func (m *MySQL) GetPollCreator(pollID string) (string, error) {
	if m.db == nil {
		return "", ErrMySQLNotConfigured
	}

	if len(pollID) > MySQLMaxLengthID {
		return "", ErrMySQLIDtooLong
	}

	rows, err := m.db.Query("SELECT creator FROM poll WHERE name=?", pollID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	if !rows.Next() {
		return "", ErrMySQLUnknownID
	}
	var c sql.NullString
	err = rows.Scan(&c)
	if err != nil {
		return "", err
	}
	if !c.Valid {
		return "", nil
	}
	return c.String, nil
}

func (m *MySQL) MarkPollDeleted(pollID string) error {
	if m.db == nil {
		return ErrMySQLNotConfigured
	}

	if len(pollID) > MySQLMaxLengthID {
		return ErrMySQLIDtooLong
	}

	_, err := m.db.Exec("UPDATE poll SET deleted=? WHERE name=?", true, pollID)
	if err != nil {
		return err
	}
	return nil
}

func (m *MySQL) GetChange(pollID, answerID string) (string, error) {
	if m.db == nil {
		return "", ErrMySQLNotConfigured
	}

	if len(pollID) > MySQLMaxLengthID {
		return "", ErrMySQLIDtooLong
	}

	var id int64
	id, err := strconv.ParseInt(answerID, 10, 64)
	if err != nil {
		return "", fmt.Errorf("mysql: can not convert id '%s': %w", answerID, err)
	}

	rows, err := m.db.Query("SELECT `change` FROM result WHERE poll=? AND id=?", pollID, id)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	if !rows.Next() {
		return "", ErrMySQLUnknownID
	}
	var c sql.NullString
	err = rows.Scan(&c)
	if err != nil {
		return "", err
	}
	if !c.Valid {
		return "", nil
	}
	return c.String, nil
}

func (m *MySQL) RunGC() error {
	if m.db == nil {
		return ErrMySQLNotConfigured
	}

	_, err := m.db.Exec("DELETE FROM poll WHERE deleted=?", true)
	if err != nil {
		return err
	}
	return nil
}

func (m *MySQL) LoadConfig(data []byte) error {
	m.dsn = string(data)
	db, err := sql.Open("mysql", m.dsn)
	if err != nil {
		return fmt.Errorf("mysql: can not open '%s': %w", m.dsn, err)
	}
	m.db = db
	return nil
}

func (m *MySQL) FlushAndClose() {
	if m.db == nil {
		return
	}

	err := m.db.Close()
	if err != nil {
		log.Printf("mysql: error closing db: %s", err.Error())
	}
}
