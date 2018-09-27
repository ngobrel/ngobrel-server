// package ngobrel provides conversations records
package ngobrel

import (
	"database/sql"
	"fmt"
	"time"

	"os"

	_ "github.com/lib/pq"
	uuid "github.com/satori/go.uuid"
)

var db *sql.DB

func (req *PutMessageRequest) putMessageToUserID(senderID uuid.UUID, senderDeviceID uuid.UUID, recipientID uuid.UUID) error {
	/*
		CREATE TABLE devices (
		  user_id UUID not null,
		  device_id UUID not null,
		  created_at INT not null,
		  updated_at INT not null,
		  device_state SMALLINT not null,
		  PRIMARY KEY (user_id, device_id)
		);o*/
	if req.MessageEncrypted == false {
		rows, err := db.Query(`SELECT device_id FROM devices WHERE user_id=$1 AND device_state = 1`, recipientID.String())
		if err != nil {
			fmt.Println("err: " + err.Error())
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var deviceID uuid.UUID
			if err := rows.Scan(&deviceID); err != nil {
				return err
			}
			err = req.putMessageToDeviceID(senderID, senderDeviceID, deviceID)
			if err != nil {
				return err
			}
		}
	} else {
		// XXX TODO Encrypted version
	}
	return nil
}

func (req *PutMessageRequest) putMessageToDeviceID(senderID uuid.UUID, senderDeviceID uuid.UUID, recipientDeviceID uuid.UUID) error {
	/*
			CREATE TABLE conversations (
		  recipient_id UUID not null,
		  message_id INT not null,
		  sender_id UUID not null,
		  sender_device_id UUID not null,
		  recipient_device_id UUID not null,
		  message_timestamp INT not null,
		  message_contents text,
		  message_encrypted boolean,
		  PRIMARY KEY (message_id, sender_id, recipient_device_id)
		);

	*/
	_, err := db.Exec(`INSERT INTO conversations values ($1, $2, $3, $4, $5, to_timestamp($6), $7, $8)`,
		req.RecipientID, req.MessageID,
		senderID.String(), senderDeviceID.String(), recipientDeviceID.String(),
		req.MessageTimestamp, req.MessageContents, req.MessageEncrypted)

	if err != nil {
		fmt.Println(req.MessageID)
		fmt.Println(err.Error())
		return err
	}
	return nil
}

func (req *GetMessagesRequest) getMessages(recipientDeviceID uuid.UUID, stream Ngobrel_GetMessagesServer) error {
	/*
			CREATE TABLE conversations (
		  recipient_id UUID not null,
		  message_id INT not null,
		  sender_id UUID not null,
		  sender_device_id UUID not null,
		  recipient_device_id UUID not null,
		  message_timestamp INT not null,
		  message_contents text,
		  message_encrypted boolean,
		  PRIMARY KEY (message_id, sender_id, recipient_device_id)
		);
	*/
	fmt.Println("Getting messages for device id" + recipientDeviceID.String())
	rows, err := db.Query(`DELETE FROM conversations WHERE recipient_device_id=$1 RETURNING recipient_id, message_id, sender_id, 
	sender_device_id, message_timestamp, message_contents, message_encrypted`, recipientDeviceID.String())
	if err != nil {
		fmt.Println(err.Error())
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var recipientID uuid.UUID
		var senderID uuid.UUID
		var senderDeviceID uuid.UUID
		var messageID int64
		var messageTimestamp time.Time
		var messageContents string
		var messageEncrypted bool

		if err := rows.Scan(&recipientID,
			&messageID,
			&senderID,
			&senderDeviceID,
			&messageTimestamp,
			&messageContents,
			&messageEncrypted); err != nil {
			fmt.Println(err.Error())
			return err
		}

		err = stream.Send(&GetMessagesResponseItem{
			RecipientID:      recipientID.String(),
			SenderID:         senderID.String(),
			SenderDeviceID:   senderDeviceID.String(),
			MessageID:        messageID,
			MessageTimestamp: int64(messageTimestamp.UnixNano() / 1000000),
			MessageContents:  messageContents,
			MessageEncrypted: messageEncrypted,
		})
		if err != nil {
			fmt.Println(err.Error())
			return err
		}
	}

	return nil
}

/**
CREATE TABLE chat_list (
  user_id UUID not null,
  chat_id INT not null,
  chat_type SMALLINT not null,
  created_at INT not null,
  updated_at INT not null,
  notification INT not null,
  PRIMARY KEY (user_id, chat_id)
);
*/

func (req *CreateConversationRequest) CreateConversation(userID uuid.UUID) (*CreateConversationResponse, error) {
	_, err := db.Exec(`INSERT INTO chat_list values ($1, $2, $3, now(), now(), $4)`,
		userID.String(), req.ChatID, req.Type, 0)

	if err != nil {
		return nil, err
	}
	return &CreateConversationResponse{ChatID: req.ChatID, Message: ""}, nil
}

func (req *UpdateConversationRequest) UpdateConversation(userID uuid.UUID) (*UpdateConversationResponse, error) {
	_, err := db.Exec(`UPDATE chat_list SET excerpt=$1, updated_at=to_timestamp($2) WHERE chat_id=$3 AND user_id=$4`,
		req.Excerpt, req.Timestamp, req.ChatID, userID.String())

	if err != nil {
		fmt.Println("error:" + err.Error())
		return nil, err
	}

	return &UpdateConversationResponse{Success: true, Message: ""}, nil
}

func (req *ListConversationsRequest) ListConversations(userID uuid.UUID) (*ListConversationsResponse, error) {
	rows, err := db.Query(`SELECT b.excerpt as excerpt, a.chat_id as chat_id, a.name as chat_name, a.chat_type as chat_type, a.notification as notification, b.updated_at 
	FROM chat_list b, contacts a
	WHERE a.chat_id = b.chat_id and a.user_id = b.user_id and a.user_id=$1 ORDER BY b.updated_at DESC`, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*Conversations = []*Conversations{}
	for rows.Next() {
		var chatID uuid.UUID
		var chatType int
		var chatName string
		var excerpt string
		var notification int64
		var updatedAt time.Time

		if err := rows.Scan(&excerpt, &chatID,
			&chatName,
			&chatType,
			&notification, &updatedAt); err != nil {
			return nil, err
		}

		item := &Conversations{
			ChatID:       chatID.String(),
			Timestamp:    updatedAt.UnixNano() / 1000000,
			Notification: int64(notification),
			ChatName:     chatName,
			Excerpt:      excerpt,
		}
		list = append(list, item)
	}
	result := &ListConversationsResponse{
		List: list,
	}
	return result, nil
}

/**
CREATE TABLE contacts (
  user_id UUID not null,
  chat_id INT not null,
  chat_type SMALLINT not null,
  name text,
  created_at INT not null,
  updated_at INT not null,
  notification INT not null,
  PRIMARY KEY (user_id, chat_id)
);

*/
func (req *GetContactsRequest) GetContacts(userID uuid.UUID) (*GetContactsResponse, error) {
	rows, err := db.Query(`SELECT chat_id, name, updated_at, notification 
	FROM contacts 
	WHERE user_id=$1 AND chat_type=0 ORDER BY name`, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*Contacts
	for rows.Next() {
		var peerID uuid.UUID
		var chatName string
		var updatedAt time.Time
		var notification int64

		if err := rows.Scan(&peerID,
			&chatName,
			&updatedAt,
			&notification); err != nil {
			return nil, err
		}

		item := &Contacts{
			PeerID:       peerID.String(),
			Name:         chatName,
			Notification: int64(notification),
		}
		fmt.Println(item)
		list = append(list, item)
	}
	result := &GetContactsResponse{
		List: list,
	}
	return result, nil
}

func InitDB() {
	connStr := os.Getenv("DB_URL")
	fmt.Println("COnnecting to DB " + connStr)
	db, _ = sql.Open("postgres", connStr)
}
