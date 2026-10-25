// Copyright 2026 The Krobar Authors.
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

// Package exitcode lets commands request specific process exit codes by
// returning an Error value; main maps it via errors.As.
package exitcode

// Exit codes used by krobar.
const (
	// Success is a clean run.
	Success = 0
	// Failed is a generic failure or invalid input.
	Failed = 1
	// Findings means the command completed but found issues (e.g. fmt --check).
	Findings = 2
)

// Error carries a desired process exit code.
type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// New wraps err in an Error with the given code.
func New(code int, err error) *Error {
	return &Error{Code: code, Err: err}
}
