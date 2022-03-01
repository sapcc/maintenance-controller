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
	"time"

	"github.com/elastic/go-ucfg"
	"github.com/sapcc/maintenance-controller/plugin"
)

type Wait struct {
	Duration time.Duration
}

func (w *Wait) New(config *ucfg.Config) (plugin.Checker, error) {
	conf := struct {
		Duration string `config:"duration" validate:"required"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	duration, err := time.ParseDuration(conf.Duration)
	if err != nil {
		return nil, err
	}
	return &Wait{Duration: duration}, nil
}

func (w *Wait) Check(params plugin.Parameters) (bool, error) {
	if time.Now().UTC().Sub(params.LastTransition) > w.Duration {
		return true, nil
	}
	return false, nil
}

func (w *Wait) AfterEval(chainResult bool, params plugin.Parameters) error {
	return nil
}
