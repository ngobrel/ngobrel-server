package ngobrel

import (
	"encoding/json"
	fmt "fmt"
	"io/ioutil"
	"log"
	"strings"
	"time"

	uuid "github.com/satori/go.uuid"
)

type FCMNotificationMessage struct {
	Message messageBody `json:"message"`
}

type messageBody struct {
	Token        string          `json:"token"`
	Notification messageContents `json:"notification"`
	Data         dataContents    `json:"data"`
}

type messageContents struct {
	Body  string `json:"body"`
	Title string `json:"title"`
}

type dataContents struct {
	ClickAction string `json:"click_action"`
	ChatID      string `json:"chatID"`
	RecipientID string `json:"recipientID"`
	Timestamp   string `json:"timestamp"`
}

func (srv *Server) sendFCM(chatID string, sender string, recipient string, excerpt string, now int64) {
	fcmToken, err := redisClient.Get("FCM-" + recipient).Result()
	if err != nil {
		log.Println("Error getting FCM token of ", recipient)
		log.Println(err)
		return
	}
	if fcmToken != "" {

		msg := &FCMNotificationMessage{
			Message: messageBody{
				Token: fcmToken,
				Notification: messageContents{
					Body:  excerpt,
					Title: sender,
				},
				Data: dataContents{
					ChatID:      chatID,
					RecipientID: recipient,
					ClickAction: "FLUTTER_NOTIFICATION_CLICK",
					Timestamp:   fmt.Sprintf("%s", now),
				},
			},
		}
		str, err := json.Marshal(msg)

		if err != nil {
			log.Println(err)
			return
		}
		log.Println("fcm", string(str))
		msgx := strings.NewReader(string(str))

		resp, err := srv.fcmAuth.client.Post(ProjectURL, "application/json", msgx)
		if err != nil {
			log.Println(err)
			return
		}
		log.Println(resp)
		bodyBytes, _ := ioutil.ReadAll(resp.Body)

		log.Println(string(bodyBytes))

	}
}

func (req *RegisterFCMRequest) RegisterFCM(userID uuid.UUID) (*RegisterFCMResponse, error) {
	log.Println("Registering FCM for ", userID.String())
	err := redisClient.Set("FCM-"+userID.String(), req.FCMToken, time.Duration(24*time.Hour)).Err()

	if err != nil {
		log.Println("Error registering FCM token for user " + userID.String())
		log.Println(err)
	}
	return &RegisterFCMResponse{Success: true}, nil
}
