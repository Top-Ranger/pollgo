// +build ldap

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

package authenticater

import (
	"crypto/tls"
	"encoding/json"
	"fmt"

	"github.com/Top-Ranger/pollgo/registry"
	"github.com/go-ldap/ldap/v3"
)

func init() {
	err := registry.RegisterAuthenticater(&LDAPUserMode{}, "LDAP-usermode")
	if err != nil {
		panic(err)
	}
}

// LDAPUserMode is an authenticator for using LDAP in user mode.
// It creates a new connection for every call to Authenticate and tries to bind the user.
type LDAPUserMode struct {
	// The endpoint of the LDAP server. Supports ldap://, ldaps://, ldapi://
	Endpoint string

	// Whether to use StartTLS. Must be disabled on encrypted connections.
	UseStartTLS bool

	// Pattern for the initial bind. Must contain a single %s which is replaced by the username.
	BindUserPattern string

	// Time limit for the LDAP search
	TimeLimit int

	// Search base dn used for searching user DN.
	BaseDN string

	// Filter used in LDAP search to find the user. Must contain a single %s which is replaced by the username.
	LDAPUserFilter string

	// If set to true, certificate validation will be skipped.
	// Only set this to true if you absolutely must and have a secure connection, otherwise user data (including passwords) might be leaked!
	// If you are unsure, set it to false.
	InsecureSkipCertificateVerify bool
}

// LoadConfig loads the LDAP configuration as a JSON.
func (l *LDAPUserMode) LoadConfig(b []byte) error {
	err := json.Unmarshal(b, l)
	if err != nil {
		return err
	}

	// Test connection
	conn, err := ldap.DialURL(l.Endpoint, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: l.InsecureSkipCertificateVerify}))
	if err != nil {
		return err
	}
	defer conn.Close()

	if l.UseStartTLS {
		err = conn.StartTLS(nil)
		if err != nil {
			return err
		}
	}

	return nil
}

// Authenticate verifies a user / password combination by binding it to the LDAP server.
func (l *LDAPUserMode) Authenticate(user, password string) (bool, error) {
	// Connect
	conn, err := ldap.DialURL(l.Endpoint, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: l.InsecureSkipCertificateVerify}))
	if err != nil {
		return false, err
	}
	defer conn.Close()

	if l.UseStartTLS {
		err = conn.StartTLS(nil)
		if err != nil {
			return false, err
		}
	}

	err = conn.Bind(fmt.Sprintf(l.BindUserPattern, user), password)
	if err != nil {
		return false, err
	}

	// Get User
	searchRequest := ldap.NewSearchRequest(
		l.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, l.TimeLimit, false,
		fmt.Sprintf(l.LDAPUserFilter, user),
		[]string{"dn"},
		nil,
	)

	searchResults, err := conn.Search(searchRequest)
	if err != nil {
		return false, err
	}

	if len(searchResults.Entries) != 1 {
		return false, nil
	}

	dn := searchResults.Entries[0].DN

	// Bind to user
	err = conn.Bind(dn, password)
	if err != nil {
		return false, nil
	}

	return true, nil
}
