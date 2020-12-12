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

function newPollObject() {
    return {};
}

function getPolls() {
    try {
        let a = JSON.parse(localStorage.getItem("pollgo_star"));
        //let version = localStorage.getItem("pollgo_star_version"); // Version not used yet.

        if (a == null) {
            return {};
        }

        if (Array.isArray(a)) {
            // Old format, we need to convert it
            let newA = {};
            for(let i = 0; i < a.length; i++) {
                newA[a[i]] = newPollObject();
            }
            a = newA;
        }
        return a
    } catch (e) {
        // Something went wrong, just return an empty object
        return {};
    }
}

function savePolls(polls) {
    try {
        let a = JSON.stringify(polls);
        localStorage.setItem("pollgo_star_version", 1);
        localStorage.setItem("pollgo_star", a);
    } catch (e) {
        console.log("error saving polls:", e);
    }
}