// Copyright 2018 The nats-operator Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package strings

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// HashSlice creates an unique identifier for a given slice of strings.
func HashSlice(s []string) string {
	h := sha256.New()
	h.Write([]byte(strings.Join(s[:], ",")))
	return hex.EncodeToString(h.Sum(nil))
}
