// Copyright 2019 The OpenZipkin Authors
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

package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// unmarshal errors
var (
	ErrValidTraceIDRequired  = errors.New("valid traceId required")
	ErrValidIDRequired       = errors.New("valid span id required")
	ErrValidDurationRequired = errors.New("valid duration required")
)

// SpanContext holds the context of a Span.
type SpanContext struct {
	TraceID  TraceID `json:"traceId"`
	ID       ID      `json:"id"`
	ParentID *ID     `json:"parentId,omitempty"`
	Debug    bool    `json:"debug,omitempty"`
	Sampled  *bool   `json:"-"`
	Err      error   `json:"-"`
}

// SpanModel structure.
//
// If using this library to instrument your application you will not need to
// directly access or modify this representation. The SpanModel is exported for
// use cases involving 3rd party Go instrumentation libraries desiring to
// export data to a Zipkin server using the Zipkin V2 Span model.
type SpanModel struct {
	SpanContext
	Name           string            `json:"name,omitempty"`
	Kind           Kind              `json:"kind,omitempty"`
	Timestamp      time.Time         `json:"-"`
	Duration       time.Duration     `json:"-"`
	Shared         bool              `json:"shared,omitempty"`
	LocalEndpoint  *Endpoint         `json:"localEndpoint,omitempty"`
	RemoteEndpoint *Endpoint         `json:"remoteEndpoint,omitempty"`
	Annotations    []Annotation      `json:"annotations,omitempty"`
	Tags           map[string]string `json:"tags,omitempty"`
}

// MarshalJSON exports our Model into the correct format for the Zipkin V2 API.
func (s SpanModel) MarshalJSON() ([]byte, error) {
	type Alias SpanModel

	var timestamp int64
	if !s.Timestamp.IsZero() {
		if s.Timestamp.Unix() < 1 {
			// Zipkin does not allow Timestamps before Unix epoch
			return nil, ErrValidTimestampRequired
		}
		timestamp = s.Timestamp.Round(time.Microsecond).UnixNano() / 1e3
	}

	if s.Duration < time.Microsecond {
		if s.Duration < 0 {
			// negative duration is not allowed and signals a timing logic error
			return nil, ErrValidDurationRequired
		} else if s.Duration > 0 {
			// sub microsecond durations are reported as 1 microsecond
			s.Duration = 1 * time.Microsecond
		}
	} else {
		// Duration will be rounded to nearest microsecond representation.
		//
		// NOTE: Duration.Round() is not available in Go 1.8 which we still support.
		// To handle microsecond resolution rounding we'll add 500 nanoseconds to
		// the duration. When truncated to microseconds in the call to marshal, it
		// will be naturally rounded. See TestSpanDurationRounding in span_test.go
		s.Duration += 500 * time.Nanosecond
	}

	if s.LocalEndpoint.Empty() {
		s.LocalEndpoint = nil
	}

	if s.RemoteEndpoint.Empty() {
		s.RemoteEndpoint = nil
	}

	return json.Marshal(&struct {
		T int64 `json:"timestamp,omitempty"`
		D int64 `json:"duration,omitempty"`
		Alias
	}{
		T:     timestamp,
		D:     s.Duration.Nanoseconds() / 1e3,
		Alias: (Alias)(s),
	})
}

// UnmarshalJSON imports our Model from a Zipkin V2 API compatible span
// representation.
func (s *SpanModel) UnmarshalJSON(b []byte) error {
	type Alias SpanModel
	span := &struct {
		T json.Number `json:"timestamp,omitempty"`
		D json.Number `json:"duration,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(s),
	}
	if err := json.Unmarshal(b, &span); err != nil {
		return err
	}
	if s.ID < 1 {
		return ErrValidIDRequired
	}
	ts, err := getInt64(span.T)
	if err != nil {
		return err
	}
	if ts > 0 {
		s.Timestamp = time.Unix(0, ts * 1e3)
	}
	dur, err := getInt64(span.D)
	if err != nil {
		return err
	}
	s.Duration = time.Duration(dur*1e3) * time.Nanosecond
	if s.LocalEndpoint.Empty() {
		s.LocalEndpoint = nil
	}

	if s.RemoteEndpoint.Empty() {
		s.RemoteEndpoint = nil
	}
	return nil
}

func getInt64(number json.Number) (value int64, err error) {
	// try int64, then float64, then string. What is the optimal order be here?
	if v, err := number.Int64(); err == nil {
		return v, nil
	}
	if v, err := number.Float64(); err == nil { // this is in case of scientific notation: 1.57967750526223e+15.
		return int64(v), nil
	}
	strVal := number.String()
	if len(strVal) == 0 { // TODO is this really a valid case? according to the tests it is
		return 0, nil
	}
	if v, err := strconv.Atoi(number.String()); err == nil {
		value = int64(v)
	}
	return 0, errors.New(fmt.Sprintf("couldn't parse %v as an integer", number.String()))
}
