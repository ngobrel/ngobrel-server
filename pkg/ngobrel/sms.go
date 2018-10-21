package ngobrel

import (
	"encoding/json"
	"errors"
	fmt "fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
)

type Sms interface {
	SetAccount(userID string, tokenID string) error
	SendMessage(from string, to string, message string) error
}

type TwilioSms struct {
	userID  string
	tokenID string
}

type DummySms struct {
}

func NewDummySms() *DummySms {
	t := &DummySms{}
	return t
}

func (t *DummySms) SetAccount(userID string, tokenID string) error {
	return nil
}

func (t *DummySms) SendMessage(from string, to string, message string) error {
	return nil
}

func NewTwilioSms() *TwilioSms {
	t := &TwilioSms{}

	return t
}

func (t *TwilioSms) SetAccount(userID string, tokenID string) error {
	t.userID = userID
	t.tokenID = tokenID

	return nil
}

func (t *TwilioSms) SendMessage(from string, to string, message string) error {

	if t.userID == "" || t.tokenID == "" {
		err := errors.New("twilio-account-not-yet-setup")
		log.Println(err)
		return err
	}

	urlStr := "https://api.twilio.com/2010-04-01/Accounts/" + t.userID + "/Messages.json"

	msgData := url.Values{}
	msgData.Set("To", to)
	msgData.Set("From", from)
	msgData.Set("Body", message)
	msgDataReader := *strings.NewReader(msgData.Encode())

	fmt.Println(msgData)
	client := &http.Client{}
	req, _ := http.NewRequest("POST", urlStr, &msgDataReader)
	req.SetBasicAuth(t.userID, t.tokenID)
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error connecting to Twilio")
		log.Println(err)
		return err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var data map[string]interface{}
		decoder := json.NewDecoder(resp.Body)
		err := decoder.Decode(&data)
		if err != nil {
			log.Println(err)
			return err
		}
		log.Println(data["sid"])
	} else {
		log.Println(resp)
		err := errors.New("twilio-unable-to-send-sms")
		log.Println(err)
		return err
	}
	if err != nil {
		log.Println(err)
		return err
	}

	return nil
}
