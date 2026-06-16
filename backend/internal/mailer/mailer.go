package mailer

import (
	"fmt"
	"mime"
	"net/smtp"
	"strconv"
	"strings"
)

type Client struct {
	Host     string
	Port     string
	Username string
	Password string
	To       string
}

func New(host, port, username, password, to string) *Client {
	return &Client{
		Host:     strings.TrimSpace(host),
		Port:     strings.TrimSpace(port),
		Username: strings.TrimSpace(username),
		Password: strings.ReplaceAll(strings.TrimSpace(password), " ", ""),
		To:       strings.TrimSpace(to),
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.Host != "" && c.Port != "" && c.Username != "" && c.Password != "" && c.To != ""
}

func (c *Client) Send(subject, body string) error {
	if !c.Enabled() {
		return nil
	}
	if _, err := strconv.Atoi(c.Port); err != nil {
		return fmt.Errorf("invalid smtp port: %w", err)
	}

	addr := c.Host + ":" + c.Port
	auth := smtp.PlainAuth("", c.Username, c.Password, c.Host)
	encodedSubject := mime.QEncoding.Encode("utf-8", subject)

	message := strings.Join([]string{
		"From: AgroCRM <" + c.Username + ">",
		"To: " + c.To,
		"Subject: " + encodedSubject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		body,
	}, "\r\n")

	return smtp.SendMail(addr, auth, c.Username, []string{c.To}, []byte(message))
}
