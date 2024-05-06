// Copyright 2024 SAP SE
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

package otelr

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel/log"
)

func NewOtelSink(otelLog log.Logger) logr.LogSink {
	return &otelSink{
		keysAndValues: make([]any, 0),
		logger:        otelLog,
	}
}

type otelSink struct {
	name          string
	keysAndValues []any
	logger        log.Logger
	callDepth     int
	level         int
}

// Init receives optional information about the logr library for LogSink
// implementations that need it.
func (os *otelSink) Init(info logr.RuntimeInfo) {
	os.callDepth = info.CallDepth
}

// Enabled tests whether this LogSink is enabled at the specified V-level.
// For example, commandline flags might be used to set the logging
// verbosity and disable some info logs.
func (os *otelSink) Enabled(level int) bool {
	// TODO: call otel logger with reverse severity
	return level <= os.level
}

// Info logs a non-error message with the given key/value pairs as context.
// The level argument is provided for optional logging.  This method will
// only be called when Enabled(level) is true. See Logger.Info for more
// details.
func (os *otelSink) Info(level int, msg string, keysAndValues ...any) {
	record := log.Record{}
	record.SetObservedTimestamp(time.Now())
	record.SetBody(log.StringValue(msg))
	record.SetSeverity(log.SeverityInfo)
	os.attachKeyAndValues(&record, keysAndValues...)
	os.logger.Emit(context.TODO(), record)
}

// Error logs an error, with the given message and key/value pairs as
// context.  See Logger.Error for more details.
func (os *otelSink) Error(err error, msg string, keysAndValues ...any) {
	record := log.Record{}
	record.SetObservedTimestamp(time.Now())
	record.SetBody(log.StringValue(msg))
	record.SetSeverity(log.SeverityError)
	record.AddAttributes(log.String("error", err.Error()))
	os.attachKeyAndValues(&record, keysAndValues...)
	os.logger.Emit(context.TODO(), record)
}

func (os *otelSink) attachKeyAndValues(record *log.Record, keysAndValues ...any) {
	record.AddAttributes(log.String("name", os.name))
	kvs := append(keysAndValues, os.keysAndValues...)
	for i := 0; i < len(kvs); i += 2 {
		key := kvs[i]
		val := kvs[i+1]
		attr := log.String(fmt.Sprintf("%v", key), fmt.Sprintf("%v", val))
		record.AddAttributes(attr)
	}
}

// WithValues returns a new LogSink with additional key/value pairs.  See
// Logger.WithValues for more details.
func (os *otelSink) WithValues(keysAndValues ...any) logr.LogSink {
	return &otelSink{
		name:          os.name,
		keysAndValues: append(os.keysAndValues, keysAndValues...),
		logger:        os.logger,
		callDepth:     os.callDepth,
		level:         os.level,
	}
}

// WithName returns a new LogSink with the specified name appended.  See
// Logger.WithName for more details.
func (os *otelSink) WithName(name string) logr.LogSink {
	newName := name
	if os.name != "" {
		newName = os.name + "." + newName
	}
	return &otelSink{
		name:          newName,
		keysAndValues: os.keysAndValues,
		logger:        os.logger,
		callDepth:     os.callDepth,
		level:         os.level,
	}
}

type multiSink struct {
	sinks []logr.LogSink
}

// Init receives optional information about the logr library for LogSink
// implementations that need it.
func (ms *multiSink) Init(info logr.RuntimeInfo) {
	for _, sink := range ms.sinks {
		sink.Init(info)
	}
}

func (ms *multiSink) Enabled(level int) bool {
	for _, sink := range ms.sinks {
		if sink.Enabled(level) {
			return true
		}
	}
	return false
}

func (ms *multiSink) Info(level int, msg string, keysAndValues ...any) {
	for _, sink := range ms.sinks {
		sink.Info(level, msg, keysAndValues...)
	}
}

func (ms *multiSink) Error(err error, msg string, keysAndValues ...any) {
	for _, sink := range ms.sinks {
		sink.Error(err, msg, keysAndValues...)
	}
}

// WithValues returns a new LogSink with additional key/value pairs.  See
// Logger.WithValues for more details.
func (ms *multiSink) WithValues(keysAndValues ...any) logr.LogSink {
	nextSinks := make([]logr.LogSink, len(ms.sinks))
	for i, sink := range ms.sinks {
		nextSinks[i] = sink.WithValues(keysAndValues...)
	}
	return &multiSink{
		sinks: nextSinks,
	}
}

// WithName returns a new LogSink with the specified name appended.  See
// Logger.WithName for more details.
func (ms *multiSink) WithName(name string) logr.LogSink {
	nextSinks := make([]logr.LogSink, len(ms.sinks))
	for i, sink := range ms.sinks {
		nextSinks[i] = sink.WithName(name)
	}
	return &multiSink{
		sinks: nextSinks,
	}
}

func NewMultiSink(sinks []logr.LogSink) logr.LogSink {
	return &multiSink{
		sinks: sinks,
	}
}
