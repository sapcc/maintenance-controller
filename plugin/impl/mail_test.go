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
	"net"
	"time"

	"github.com/elastic/go-ucfg/yaml"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/plugin"
)

var _ = Describe("The mail plugin", func() {

	It("should parse its config", func() {
		configStr := "auth: true\naddress: addr\nfrom: from\nidentity: ident\nmessage: msg\npassword: pw\nto: to\nuser: user"
		config, err := yaml.NewConfig([]byte(configStr))
		Expect(err).To(Succeed())
		var base Mail
		plugin, err := base.New(config)
		Expect(err).To(Succeed())
		mail := plugin.(*Mail)
		Expect(mail.Address).To(Equal("addr"))
		Expect(mail.Auth).To(BeTrue())
		Expect(mail.From).To(Equal("from"))
		Expect(mail.Identity).To(Equal("ident"))
		Expect(mail.Message).To(Equal("msg"))
		Expect(mail.Password).To(Equal("pw"))
		Expect(mail.To[0]).To(Equal("to"))
		Expect(mail.User).To(Equal("user"))
	})

	It("should send an email", func(done Done) {
		messageArrived := make(chan bool, 1)
		defer close(messageArrived)
		// the goroutine simulates an smtp server
		go func() {
			defer GinkgoRecover()
			listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 37953})
			Expect(err).To(Succeed())
			defer listener.Close()
			conn, err := listener.AcceptTCP()
			Expect(err).To(Succeed())
			_, err = conn.Write([]byte("220 127.0.0.1 SMTP\r\n"))
			Expect(err).To(Succeed())
			err = nil
			var request bytes.Buffer
			// the loop asembles client requests and replies to them after reading a \n
			for err == nil {
				buf := make([]byte, 1)
				_, err = conn.Read(buf)
				if string(buf) == "\n" {
					reply := "250 OK\r\n"
					switch request.String() {
					case "themessage\r":
						messageArrived <- true
						continue
					case "DATA\r":
						reply = "354 start mail input\r\n"
					case "QUIT\r":
						reply = "221 closing channel\r\n"
					}
					_, err = conn.Write([]byte(reply))
					Expect(err).To(Succeed())
					request.Reset()
				} else {
					err = request.WriteByte(buf[0])
					Expect(err).To(Succeed())
				}
			}
			Expect(err).To(HaveOccurred())
		}()
		time.Sleep(20 * time.Millisecond)
		instance := Mail{
			Auth:    false,
			Address: "localhost:37953",
			From:    "from@example.com",
			Message: "themessage",
			To:      []string{"to@example.com"},
		}
		err := instance.Notify(plugin.Parameters{})
		Expect(err).To(Succeed())
		Expect(<-messageArrived).To(BeTrue())
		close(done)
	}, 1)

})
