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
	"io/ioutil"
	"net/http"

	"github.com/elastic/go-ucfg"
	"github.com/sapcc/maintenance-controller/plugin"
)

// Slack is a notification plugin that uses a slack webhook and a channel
// to post a notification about the nodes state in slack.
type Slack struct {
	Hook    string
	Channel string
	Message string
}

// New creates a new Slack instance with the given config.
func (s *Slack) New(config *ucfg.Config) (plugin.Notifier, error) {
	conf := struct {
		Hook    string `config:"hook" validate:"required"`
		Channel string `config:"channel" validate:"required"`
		Message string `config:"message" validate:"required"`
	}{}
	err := config.Unpack(&conf)
	if err != nil {
		return nil, err
	}
	return &Slack{Hook: conf.Hook, Channel: conf.Channel, Message: conf.Message}, nil
}

// Notify performs a POST-Request to the slack API to create a message within slack.
func (s *Slack) Notify(params plugin.Parameters) error {
	theMessage, err := plugin.RenderNotificationTemplate(s.Message, params)
	if err != nil {
		return err
	}
	msg := struct {
		Text    string `json:"text"`
		Channel string `json:"channel"`
	}{Text: theMessage, Channel: s.Channel}
	marshaled, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	rsp, err := http.Post(s.Hook, "application/json", bytes.NewReader(marshaled))
	if err != nil {
		return err
	}
	defer rsp.Body.Close()
	bodyBytes, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		return err
	}
	if string(bodyBytes) != "ok" {
		return errors.New("slack webhook response is not ok")
	}
	return nil
}
