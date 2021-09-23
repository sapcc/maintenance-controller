/*

Copyright 2020 SAP SE

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package event

import (
	"fmt"
	"math/rand"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/tools/record/util"
	ref "k8s.io/client-go/tools/reference"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

const maxTriesPerEvent = 12

var defaultSleepDuration = 10 * time.Second

const maxQueuedEvents = 1000

// More or less copied from client-go.
type eventBroadcasterImpl struct {
	*watch.Broadcaster
	sleepDuration time.Duration
	options       record.CorrelatorOptions
}

// Creates a new event broadcaster.
func NewNodeBroadcaster() record.EventBroadcaster {
	return &eventBroadcasterImpl{
		Broadcaster:   watch.NewBroadcaster(maxQueuedEvents, watch.DropIfChannelFull),
		sleepDuration: defaultSleepDuration,
	}
}

// StartRecordingToSink starts sending events received from the specified eventBroadcaster to the given sink.
// The return value can be ignored or used to stop recording, if desired.
func (e *eventBroadcasterImpl) StartRecordingToSink(sink record.EventSink) watch.Interface {
	eventCorrelator := record.NewEventCorrelatorWithOptions(e.options)
	return e.StartEventWatcher(
		func(event *v1.Event) {
			recordToSink(sink, event, eventCorrelator, e.sleepDuration)
		})
}

func (e *eventBroadcasterImpl) Shutdown() {
	e.Broadcaster.Shutdown()
}

func recordToSink(sink record.EventSink, event *v1.Event,
	eventCorrelator *record.EventCorrelator, sleepDuration time.Duration) {
	// Make a copy before modification, because there could be multiple listeners.
	// Events are safe to copy like this.
	eventCopy := *event
	event = &eventCopy
	result, err := eventCorrelator.EventCorrelate(event)
	if err != nil {
		utilruntime.HandleError(err)
	}
	if result.Skip {
		return
	}
	tries := 0
	for {
		if recordEvent(sink, result.Event, result.Patch, result.Event.Count > 1, eventCorrelator) {
			break
		}
		tries++
		if tries >= maxTriesPerEvent {
			klog.Errorf("Unable to write event '%#v' (retry limit exceeded!)", event)
			break
		}
		// Randomize the first sleep so that various clients won't all be
		// synced up if the master goes down.
		if tries == 1 {
			time.Sleep(time.Duration(float64(sleepDuration) * rand.Float64())) //nolint:gosec
		} else {
			time.Sleep(sleepDuration)
		}
	}
}

// recordEvent attempts to write event to a sink. It returns true if the event
// was successfully recorded or discarded, false if it should be retried.
// If updateExistingEvent is false, it creates a new event, otherwise it updates
// existing event.
// nolint:cyclop
func recordEvent(sink record.EventSink, event *v1.Event, patch []byte,
	updateExistingEvent bool, eventCorrelator *record.EventCorrelator) bool {
	var newEvent *v1.Event
	var err error
	if updateExistingEvent {
		newEvent, err = sink.Patch(event, patch)
	}
	// Update can fail because the event may have been removed and it no longer exists.
	if !updateExistingEvent || (updateExistingEvent && util.IsKeyNotFoundError(err)) {
		// Making sure that ResourceVersion is empty on creation
		event.ResourceVersion = ""
		newEvent, err = sink.Create(event)
	}
	if err == nil {
		// we need to update our event correlator with the server returned state to handle name/resourceversion
		eventCorrelator.UpdateState(newEvent)
		return true
	}

	// If we can't contact the server, then hold everything while we keep trying.
	// Otherwise, something about the event is malformed and we should abandon it.
	switch err.(type) { // nolint: errorlint
	case *restclient.RequestConstructionError:
		// We will construct the request the same next time, so don't keep trying.
		klog.Errorf("Unable to construct event '%#v': '%v' (will not retry!)", event, err)
		return true
	case *errors.StatusError:
		if errors.IsAlreadyExists(err) {
			klog.V(5).Infof("Server rejected event '%#v': '%v' (will not retry!)", event, err) //nolint:gomnd
		} else {
			klog.Errorf("Server rejected event '%#v': '%v' (will not retry!)", event, err)
		}
		return true
	case *errors.UnexpectedObjectError:
		// We don't expect this; it implies the server's response didn't match a
		// known pattern. Go ahead and retry.
	default:
		// This case includes actual http transport errors. Go ahead and retry.
	}
	klog.Errorf("Unable to write event: '%#v': '%v'(may retry after sleeping)", event, err)
	return false
}

// StartLogging starts sending events received from this EventBroadcaster to the given logging function.
// The return value can be ignored or used to stop recording, if desired.
func (e *eventBroadcasterImpl) StartLogging(logf func(format string, args ...interface{})) watch.Interface {
	return e.StartEventWatcher(
		func(e *v1.Event) {
			logf("Event(%#v): type: '%v' reason: '%v' %v", e.InvolvedObject, e.Type, e.Reason, e.Message)
		})
}

// StartStructuredLogging starts sending events received from this EventBroadcaster to the structured logging function.
// The return value can be ignored or used to stop recording, if desired.
func (e *eventBroadcasterImpl) StartStructuredLogging(verbosity klog.Level) watch.Interface {
	return e.StartEventWatcher(
		func(e *v1.Event) {
			klog.V(verbosity).InfoS("Event occurred", "object", klog.KRef(e.InvolvedObject.Namespace, e.InvolvedObject.Name),
				"kind", e.InvolvedObject.Kind, "apiVersion", e.InvolvedObject.APIVersion,
				"type", e.Type, "reason", e.Reason, "message", e.Message)
		})
}

// StartEventWatcher starts sending events received from this EventBroadcaster to the given event handler function.
// The return value can be ignored or used to stop recording, if desired.
func (e *eventBroadcasterImpl) StartEventWatcher(eventHandler func(*v1.Event)) watch.Interface {
	watcher := e.Watch()
	go func() {
		defer utilruntime.HandleCrash()
		for watchEvent := range watcher.ResultChan() {
			event, ok := watchEvent.Object.(*v1.Event)
			if !ok {
				// This is all local, so there's no reason this should
				// ever happen.
				continue
			}
			eventHandler(event)
		}
	}()
	return watcher
}

// NewRecorder returns an EventRecorder that records events with the given event source.
func (e *eventBroadcasterImpl) NewRecorder(scheme *runtime.Scheme, source v1.EventSource) record.EventRecorder {
	return &NodeRecorder{scheme, source, e.Broadcaster, clock.RealClock{}}
}

type NodeRecorder struct {
	scheme *runtime.Scheme
	source v1.EventSource
	*watch.Broadcaster
	clock clock.Clock
}

func (recorder *NodeRecorder) generateEvent(object runtime.Object, annotations map[string]string,
	source *v1.EventSource, eventtype, reason, message string) {
	ref, err := ref.GetReference(recorder.scheme, object)
	if err != nil {
		klog.Errorf("Could not construct reference to: '%#v' due to: '%v'. Will not report event: '%v' '%v' '%v'",
			object, err, eventtype, reason, message)
		return
	}

	if !util.ValidateEventType(eventtype) {
		klog.Errorf("Unsupported event type: '%v'", eventtype)
		return
	}

	event := recorder.makeEvent(ref, annotations, eventtype, reason, message)
	if source == nil {
		event.Source = recorder.source
	} else {
		event.Source = *source
	}

	go func() {
		// NOTE: events should be a non-blocking operation
		defer utilruntime.HandleCrash()
		recorder.Action(watch.Added, event)
	}()
}

func (recorder *NodeRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	recorder.generateEvent(object, nil, nil, eventtype, reason, message)
}

func (recorder *NodeRecorder) Eventf(object runtime.Object,
	eventtype, reason, messageFmt string, args ...interface{}) {
	recorder.Event(object, eventtype, reason, fmt.Sprintf(messageFmt, args...))
}

func (recorder *NodeRecorder) AnnotatedEventf(object runtime.Object,
	annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
	recorder.generateEvent(object, annotations, nil, eventtype, reason, fmt.Sprintf(messageFmt, args...))
}

func (recorder *NodeRecorder) makeEvent(ref *v1.ObjectReference, annotations map[string]string,
	eventtype, reason, message string) *v1.Event {
	t := metav1.Time{Time: recorder.clock.Now()}
	// this makes the event appear in kubectl describe node.
	ref.UID = types.UID(ref.Name)
	namespace := ref.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceDefault
	}
	return &v1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%v.%x", ref.Name, t.UnixNano()),
			Namespace:   namespace,
			Annotations: annotations,
		},
		InvolvedObject: *ref,
		Reason:         reason,
		Message:        message,
		FirstTimestamp: t,
		LastTimestamp:  t,
		Count:          1,
		Type:           eventtype,
	}
}
