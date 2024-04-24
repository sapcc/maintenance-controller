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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"

	"github.com/sapcc/maintenance-controller/plugin"
)

var _ = Describe("The mail plugin", func() {
	It("should parse its config", func() {
		configStr := "auth: true\naddress: addr\nfrom: from\nidentity: ident\nmessage: msg\n"
		configStr += "password: pw\nto: to\nuser: user\nsubject: sub"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var base Mail
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		mail, ok := plugin.(*Mail)
		Expect(ok).To(BeTrue())
		Expect(mail.Address).To(Equal("addr"))
		Expect(mail.Auth).To(BeTrue())
		Expect(mail.From).To(Equal("from"))
		Expect(mail.Identity).To(Equal("ident"))
		Expect(mail.Message).To(Equal("msg"))
		Expect(mail.Password).To(Equal("pw"))
		Expect(mail.To[0]).To(Equal("to"))
		Expect(mail.User).To(Equal("user"))
		Expect(mail.Subject).To(Equal("sub"))
	})

	It("should build a valid header", func() {
		plugin := Mail{
			To:      []string{"To1", "To2"},
			From:    "From",
			Subject: "Sub",
		}
		header := plugin.buildMailHeader()
		Expect(header).To(Equal("From: From\r\nTo: To1,To2\r\nSubject: Sub\r\n\r\n"))
	})

	It("should send an email", func() {
		done := make(chan interface{})
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
			var request bytes.Buffer
			isDataBlock := false
			// the loop asembles client requests and replies to them after reading a \n
			for {
				buf := make([]byte, 1)
				_, err = conn.Read(buf)
				if err != nil {
					break
				}
				if string(buf) == "\n" {
					// the default reply is 250 OK
					reply := "250 OK\r\n"
					// on newlines in data block we do not need to reply
					if isDataBlock {
						reply = ""
					}
					switch request.String() {
					// assert that the message has been tranmitted
					case "themessage\r":
						messageArrived <- true
					// data block starts
					case "DATA\r":
						isDataBlock = true
						reply = "354 start mail input\r\n"
					// end of communication
					case "QUIT\r":
						reply = "221 closing channel\r\n"
					// end of data block
					case ".\r":
						reply = "250 OK\r\n"
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
			close(done)
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
		Eventually(done, 1).Should(BeClosed())
	})
})
