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
	"net/smtp"
	"strings"

	"github.com/sapcc/ucfgwrap"

	"github.com/sapcc/maintenance-controller/plugin"
)

// Mail is a notification plugins that sends an e-mail.
type Mail struct {
	Auth     bool
	Message  string
	Subject  string
	Address  string
	From     string
	To       []string
	Identity string
	User     string
	Password string
}

// New creates a new Mail instance with the given config.
func (m *Mail) New(config *ucfgwrap.Config) (plugin.Notifier, error) {
	conf := struct {
		Auth     bool     `config:"auth" validate:"required"`
		Message  string   `config:"message" validate:"required"`
		Subject  string   `config:"subject" validate:"required"`
		Address  string   `config:"address" validate:"required"`
		From     string   `config:"from" validate:"required"`
		To       []string `config:"to" validate:"required"`
		Identity string   `config:"identity"`
		User     string   `config:"user"`
		Password string   `config:"password"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &Mail{
		Auth:     conf.Auth,
		Address:  conf.Address,
		From:     conf.From,
		Identity: conf.Identity,
		Message:  conf.Message,
		Subject:  conf.Subject,
		Password: conf.Password,
		To:       conf.To,
		User:     conf.User,
	}, nil
}

func (m *Mail) ID() string {
	return "mail"
}

// Notify performs connects to the provided SMTP server and transmits the configured message.
func (m *Mail) Notify(params plugin.Parameters) error {
	theMessage, err := plugin.RenderNotificationTemplate(m.Message, &params)
	theMessage = m.buildMailHeader() + theMessage
	if err != nil {
		return err
	}
	var auth smtp.Auth
	if m.Auth {
		server := strings.Split(m.Address, ":")[0]
		auth = smtp.PlainAuth(m.Identity, m.User, m.Password, server)
	}
	err = smtp.SendMail(m.Address, auth, m.From, m.To, []byte(theMessage))
	if err != nil {
		return err
	}
	return nil
}

func (m *Mail) buildMailHeader() string {
	recipients := strings.Join(m.To, ",")
	return "From: " + m.From + "\r\nTo: " + recipients + "\r\nSubject: " + m.Subject + "\r\n\r\n"
}
