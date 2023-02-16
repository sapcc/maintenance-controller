/*******************************************************************************
*
* Copyright 2020 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package impl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/ucfgwrap"
	"github.com/slack-go/slack"
	coordinationv1 "k8s.io/api/coordination/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SlackWebhook is a notification plugin that uses a slack webhook and a channel
// to post a notification about the nodes state in slack.
type SlackWebhook struct {
	Hook    string
	Channel string
	Message string
}

// New creates a new Slack instance with the given config.
func (sw *SlackWebhook) New(config *ucfgwrap.Config) (plugin.Notifier, error) {
	conf := struct {
		Hook    string `config:"hook" validate:"required"`
		Channel string `config:"channel" validate:"required"`
		Message string `config:"message" validate:"required"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &SlackWebhook{Hook: conf.Hook, Channel: conf.Channel, Message: conf.Message}, nil
}

func (sw *SlackWebhook) ID() string {
	return "slack"
}

// Notify performs a POST-Request to the slack API to create a message within slack.
func (sw *SlackWebhook) Notify(params plugin.Parameters) error {
	theMessage, err := plugin.RenderNotificationTemplate(sw.Message, &params)
	if err != nil {
		return err
	}
	msg := struct {
		Text    string `json:"text"`
		Channel string `json:"channel"`
	}{Text: theMessage, Channel: sw.Channel}
	marshaled, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(params.Ctx, http.MethodPost, sw.Hook, bytes.NewReader(marshaled))
	req.Header.Set("Content-Type", "application/json")
	if err != nil {
		return err
	}
	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer rsp.Body.Close()
	bodyBytes, err := io.ReadAll(rsp.Body)
	if err != nil {
		return err
	}
	if string(bodyBytes) != "ok" {
		return errors.New("slack webhook response is not ok")
	}
	return nil
}

// Slack is a notification plugin that uses a slack webhook and a channel
// to post a notification about the nodes state in slack while grouping
// messages within a certain period in a thread.
type SlackThread struct {
	Token     string
	Channel   string
	Title     string
	Message   string
	LeaseName types.NamespacedName
	Period    time.Duration
	testURL   string
}

// New creates a new Slack instance with the given config.
func (st *SlackThread) New(config *ucfgwrap.Config) (plugin.Notifier, error) {
	conf := struct {
		Token          string        `config:"token" validate:"required"`
		Channel        string        `config:"channel" validate:"required"`
		Title          string        `config:"title" validate:"required"`
		Message        string        `config:"message" validate:"required"`
		LeaseName      string        `config:"leaseName" validate:"required"`
		LeaseNamespace string        `config:"leaseNamespace" validate:"required"`
		Period         time.Duration `config:"period" validate:"required"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &SlackThread{
		testURL: "",
		Token:   conf.Token,
		Channel: conf.Channel,
		Message: conf.Message,
		Title:   conf.Title,
		Period:  conf.Period,
		LeaseName: types.NamespacedName{
			Namespace: conf.LeaseNamespace,
			Name:      conf.LeaseName,
		},
	}, nil
}

func (st *SlackThread) ID() string {
	return "slackThread"
}

func (st *SlackThread) SetTestURL(url string) {
	st.testURL = url
}

func (st *SlackThread) makeSlack() *slack.Client {
	if st.testURL == "" {
		return slack.New(st.Token)
	}
	return slack.New(st.Token, slack.OptionAPIURL(st.testURL))
}

func (st *SlackThread) Notify(params plugin.Parameters) error {
	api := st.makeSlack()
	var lease coordinationv1.Lease
	err := params.Client.Get(params.Ctx, st.LeaseName, &lease)
	if k8serrors.IsNotFound(err) {
		parentTS, err := st.startThread(&params, api)
		if err != nil {
			return fmt.Errorf("failed to create slack thread: %w", err)
		}
		err = st.createLease(&params, parentTS)
		if err != nil {
			return fmt.Errorf("failed to create slack thread lease %s: %w", st.LeaseName, err)
		}
		return nil
	} else if err != nil {
		return err
	}
	// check lease
	if time.Since(lease.Spec.RenewTime.Time) <= time.Duration(*lease.Spec.LeaseDurationSeconds)*time.Second {
		// post into thread
		if lease.Spec.HolderIdentity == nil {
			return fmt.Errorf("slack thread leases has no holder")
		}
		err := st.replyMessage(&params, api, *lease.Spec.HolderIdentity)
		if err != nil {
			return fmt.Errorf("failed to reply to slack thread: %w", err)
		}
		return nil
	}
	parentTS, err := st.startThread(&params, api)
	if err != nil {
		return fmt.Errorf("failed to create slack thread: %w", err)
	}
	// update Lease
	err = st.updateLease(&params, parentTS, &lease)
	if err != nil {
		return fmt.Errorf("failed to update slack thread lease %s: %w", st.LeaseName, err)
	}
	return nil
}

func (st *SlackThread) startThread(params *plugin.Parameters, api *slack.Client) (string, error) {
	parentTS, err := st.postTitle(params, api)
	if err != nil {
		return "", fmt.Errorf("failed to post message to slack: %w", err)
	}
	err = st.replyMessage(params, api, parentTS)
	if err != nil {
		return "", fmt.Errorf("failed to reply to slack thread: %w", err)
	}
	return parentTS, nil
}

func (st *SlackThread) postTitle(params *plugin.Parameters, api *slack.Client) (string, error) {
	theMessage, err := plugin.RenderNotificationTemplate(st.Title, params)
	if err != nil {
		return "", err
	}
	_, ts, err := api.PostMessageContext(params.Ctx, st.Channel, slack.MsgOptionText(theMessage, true))
	if err != nil {
		return "", err
	}
	return ts, nil
}

func (st *SlackThread) replyMessage(params *plugin.Parameters, api *slack.Client, parentTS string) error {
	theMessage, err := plugin.RenderNotificationTemplate(st.Message, params)
	if err != nil {
		return err
	}
	_, _, err = api.PostMessageContext(params.Ctx, st.Channel,
		slack.MsgOptionText(theMessage, true), slack.MsgOptionTS(parentTS))
	if err != nil {
		return err
	}
	return nil
}

func (st *SlackThread) createLease(params *plugin.Parameters, parentTS string) error {
	var lease coordinationv1.Lease
	lease.Name = st.LeaseName.Name
	lease.Namespace = st.LeaseName.Namespace
	lease.Spec.HolderIdentity = &parentTS
	now := v1.MicroTime{
		Time: time.Now().UTC(),
	}
	lease.Spec.AcquireTime = &now
	lease.Spec.RenewTime = &now
	secs := int32(st.Period.Seconds())
	lease.Spec.LeaseDurationSeconds = &secs
	err := params.Client.Create(params.Ctx, &lease)
	return err
}

func (st *SlackThread) updateLease(params *plugin.Parameters, parentTS string, lease *coordinationv1.Lease) error {
	unmodified := lease.DeepCopy()
	lease.Spec.HolderIdentity = &parentTS
	lease.Spec.RenewTime = &v1.MicroTime{Time: time.Now().UTC()}
	secs := int32(st.Period.Seconds())
	lease.Spec.LeaseDurationSeconds = &secs
	err := params.Client.Patch(params.Ctx, lease, client.MergeFrom(unmodified))
	return err
}
