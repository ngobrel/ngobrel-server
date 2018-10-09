// package ngobrel provides conversations records
package ngobrel

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/go-redis/redis"

	"github.com/cespare/xxhash"

	"os"

	_ "github.com/lib/pq"
	uuid "github.com/satori/go.uuid"
)

var db *sql.DB
var redisClient *redis.Client

func InitDB() {
	connStr := os.Getenv("DB_URL")
	redisStr := os.Getenv("REDIS_URL")
	fmt.Println("Connecting to DB " + connStr + " and redis: " + redisStr)
	db, _ = sql.Open("postgres", connStr)

	redisClient = redis.NewClient(&redis.Options{
		Addr:     redisStr,
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	pong, err := redisClient.Ping().Result()
	log.Println(pong, err)
}

func getUserIDFromToken(token string) (string, error) {
	val, err := redisClient.Get("UID-" + token).Result()
	if err != nil {
		log.Println(err)
		return "", err
	}
	return val, nil
}

func getDeviceIDFromToken(token string) (string, error) {
	val, err := redisClient.Get("DEV-" + token).Result()
	if err != nil {
		log.Println(err)
		return "", err
	}
	return val, nil
}

func (req *PutMessageRequest) putMessageToUserIDCheckGroup(srv *Server, senderID uuid.UUID, senderDeviceID uuid.UUID, recipientID uuid.UUID, now float64) error {
	rows, err := db.Query(`SELECT chat_id FROM group_list WHERE chat_id=$1`, recipientID.String())
	if err != nil {
		fmt.Println("err: " + err.Error())
		return err
	}

	defer rows.Close()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})

	for rows.Next() {
		var groupID uuid.UUID
		if err := rows.Scan(&groupID); err != nil {
			log.Println(err)
			return err
		}

		log.Println("It's a group.")
		err = req.putMessageToGroupMember(srv, tx, senderID, senderDeviceID, groupID, now)

		if err != nil {
			log.Println(err)
			tx.Rollback()
			return err
		}

		if err = tx.Commit(); err != nil {
			log.Println(err)
		}
		return err
	}

	// not found in group list, so it must be individual recipient
	err = req.putMessageToUserID(srv, tx, false, senderID, senderDeviceID, recipientID, now)
	if err != nil {
		log.Println(err)
		tx.Rollback()
		return err
	}

	if err = tx.Commit(); err != nil {
		log.Println(err)
	}
	return err
}

func (req *PutMessageRequest) putMessageToGroupMember(srv *Server, tx *sql.Tx, senderID uuid.UUID, senderDeviceID uuid.UUID, chatID uuid.UUID, now float64) error {
	rows, err := db.Query(`SELECT user_id FROM chat_list WHERE chat_id=$1`, chatID.String())
	if err != nil {
		log.Println(err)
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var recipientID uuid.UUID
		if err := rows.Scan(&recipientID); err != nil {
			log.Println(err)
			return err
		}

		err = req.putMessageToUserID(srv, tx, true, senderID, senderDeviceID, recipientID, now)
		if err != nil {
			log.Println(err)
			return err
		}

		_, err = tx.Exec(`
		INSERT INTO chat_list (user_id, chat_id, created_at, updated_at, excerpt, chat_type) values ($3, $2, now(), now(), $1, 1) ON CONFLICT (user_id, chat_id) DO UPDATE SET excerpt=$1, updated_at=now()`,
			req.MessageExcerpt, chatID.String(), recipientID.String())

		if err != nil {
			log.Println(err)
			return err
		}
	}

	return nil
}

func (req *PutMessageRequest) putMessageToUserID(srv *Server, tx *sql.Tx, isGroup bool, senderID uuid.UUID, senderDeviceID uuid.UUID, recipientID uuid.UUID, now float64) error {
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

			err = req.putMessageToDeviceID(srv, tx, senderID, senderDeviceID, deviceID, now)
			if err != nil {
				return err
			}
		}
		if isGroup == false {
			fmt.Println("Updating chat_list")
			_, err = tx.Exec(`
			INSERT INTO chat_list  (user_id, chat_id, created_at, updated_at, excerpt) values ($3, $2, now(), now(), $1) ON CONFLICT (user_id, chat_id) DO UPDATE SET excerpt=$1, updated_at=now()`,
				req.MessageExcerpt, recipientID.String(), senderID.String())
			if err != nil {
				fmt.Println(err)
			}
		}
	} else {
		// XXX TODO Encrypted version
	}
	return nil
}

func (req *PutMessageRequest) putMessageToDeviceID(srv *Server, tx *sql.Tx, senderID uuid.UUID, senderDeviceID uuid.UUID, recipientDeviceID uuid.UUID, now float64) error {

	log.Println("putMessageToDeviceID: ", senderID.String(), req.MessageID, recipientDeviceID.String(), req.RecipientID)

	_, err := tx.Exec(`INSERT INTO conversations values ($1, $2, $3, $4, $5, to_timestamp($6), $7, $8)`,
		req.RecipientID, req.MessageID,
		senderID.String(), senderDeviceID.String(), recipientDeviceID.String(),
		now, req.MessageContents, req.MessageEncrypted)

	if err != nil {
		fmt.Println(req.MessageID)
		fmt.Println(err.Error())
		return err
	}

	stream, ok := srv.receiptStream[recipientDeviceID.String()]
	if ok && stream != nil {
		fmt.Println("Ping " + recipientDeviceID.String())
		now := time.Now().UnixNano() / 1000
		stream.Send(&GetMessageNotificationStream{Timestamp: now})
	}

	return nil
}

func (req *GetMessagesRequest) getMessageNotificationStream(srv *Server, recipientDeviceID uuid.UUID, stream Ngobrel_GetMessageNotificationServer) error {
	// subscribe
	srv.receiptStream[recipientDeviceID.String()] = stream
	log.Println(recipientDeviceID.String() + " is susbscribed")
	for {
		// suspend
		fmt.Println("Notification stream for " + recipientDeviceID.String())
		time.Sleep(5 * 60 * time.Second)
	}
	return nil
}

func (req *GetMessagesRequest) getMessages(recipientDeviceID uuid.UUID, stream Ngobrel_GetMessagesServer) error {

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

func (req *CreateConversationRequest) CreateConversation(userID uuid.UUID) (*CreateConversationResponse, error) {
	_, err := db.Exec(`INSERT INTO chat_list values ($1, $2, $3, now(), now(), $4)`,
		userID.String(), req.ChatID, req.Type, 0)

	if err != nil {
		return nil, err
	}
	return &CreateConversationResponse{ChatID: req.ChatID, Message: ""}, nil
}

func (req *UpdateConversationRequest) UpdateConversation(userID uuid.UUID) (*UpdateConversationResponse, error) {
	_, err := db.Exec(`
		INSERT INTO chat_list values ($4, $3, now(), now(), $1) 
		ON CONFLICT (user_id, chat_id) DO
	UPDATE SET excerpt=$1, updated_at=to_timestamp($2)`,
		req.Excerpt, req.Timestamp, req.ChatID, userID.String())

	if err != nil {
		fmt.Println("error:" + err.Error())
		return nil, err
	}

	return &UpdateConversationResponse{Success: true, Message: ""}, nil
}

func (req *ListConversationsRequest) ListConversations(userID uuid.UUID) (*ListConversationsResponse, error) {
	fmt.Println(userID.String())

	rows, err := db.Query(`
	SELECT b.chat_type, b.excerpt, a.chat_id, a.title as chat_name, b.updated_at FROM group_list a, chat_list b WHERE a.chat_id = b.chat_id and b.user_id=$1
	UNION ALL
	SELECT b.chat_type, b.excerpt, a.chat_id, a.name as chat_name,  b.updated_at FROM contacts a, chat_list b WHERE a.chat_id = b.chat_id and a.user_id = b.user_id and b.user_id=$1
	ORDER BY updated_at DESC
	`, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*Conversations = []*Conversations{}
	for rows.Next() {
		var chatID uuid.UUID
		var chatType int32
		var chatName string
		var excerpt string

		//var notification int64
		var updatedAt time.Time

		if err := rows.Scan(&chatType, &excerpt, &chatID,
			&chatName,
			&updatedAt); err != nil {
			return nil, err
		}

		item := &Conversations{
			ChatID:    chatID.String(),
			Timestamp: updatedAt.UnixNano() / 1000000,
			//Notification: int64(notification),
			ChatName: chatName,
			Excerpt:  excerpt,
			ChatType: chatType,
		}
		list = append(list, item)
	}
	result := &ListConversationsResponse{
		List: list,
	}
	log.Println(result)

	return result, nil
}

func (req *CreateProfileRequest) CreateProfile(srv *Server) (*CreateProfileResponse, error) {
	rows, err := db.Query(`SELECT user_id FROM profile where phone_number=$1`, req.PhoneNumber)
	if err != nil {
		fmt.Println(err.Error())
		return nil, err
	}

	defer rows.Close()
	var userID string
	for rows.Next() {
		err := rows.Scan(&userID)
		if err != nil {
			fmt.Println(err.Error())
			return nil, err
		}
	}

	if len(userID) == 0 {
		newUUID := uuid.Must(uuid.NewV4(), nil)
		userID = newUUID.String()
		_, err = db.Exec(`INSERT INTO profile (user_id, phone_number, created_at, updated_at) values ($1, $2, now(), now());`, userID, req.PhoneNumber)
		if err != nil {
			fmt.Println(err.Error())
			return nil, err
		}
	}

	fmt.Println(userID + ":" + req.DeviceID + ":" + req.PhoneNumber)
	_, err = db.Exec(`INSERT INTO devices (user_id, device_id, updated_at, created_at, device_state) values ($1, $2, now(), now(), 0)
	ON CONFLICT (user_id, device_id) DO UPDATE SET device_state=0, user_id=$1, device_id=$2, updated_at=now() 
	`, userID, req.DeviceID)
	if err != nil {
		fmt.Println(err.Error())
		return nil, err
	}

	now := strconv.FormatInt(time.Now().UnixNano(), 10)
	nowSlice := now[len(now)-4 : len(now)]
	otpCode := fmt.Sprintf("%s", nowSlice)
	//	otpCode = "1234" // XXX
	otpHash := xxhash.Sum64String(req.PhoneNumber+otpCode) &^ (1 << 63)

	smsMessage := fmt.Sprintf(SmsMessage, otpCode)
	srv.smsClient.SendMessage(SmsSender, req.PhoneNumber, smsMessage)

	log.Println("HASH:" + otpCode)
	_, err = db.Exec(`INSERT INTO otp (otp_code, expired_at) values ($1, now() + interval '1 day')`, int64(otpHash))
	if err != nil {
		fmt.Println(err.Error())
		return nil, err
	}

	if DebugMode == false {
		otpCode = ""
	}
	return &CreateProfileResponse{
		UserID:   userID,
		OtpDebug: otpCode, // DEBUG Mode
	}, nil
}

func (req *EditProfileRequest) EditProfile(userID uuid.UUID) (*EditProfileResponse, error) {
	result, err := db.Exec(`UPDATE profile set user_name=$1, name=$2, custom_data=$3, updated_at=now() WHERE user_id=$4;`,
		req.UserName, req.Name, req.CustomData, userID.String())

	if err != nil {
		fmt.Println(err.Error())
		return nil, err
	}

	count, err := result.RowsAffected()
	if err != nil {
		fmt.Println("EditProfileError: " + err.Error())
		return nil, err
	}
	return &EditProfileResponse{
		Success: count > 0,
	}, nil
}

func (req *GetProfileRequest) GetProfile(userID uuid.UUID) (*GetProfileResponse, error) {
	qUserID := userID.String()
	if req.UserID != "" {
		qUserID = req.UserID
	}
	rows, err := db.Query(`SELECT name, phone_number, user_name, custom_data, updated_at from profile WHERE user_id=$1;`,
		qUserID)
	if err != nil {
		fmt.Println(err.Error())
		return nil, err
	}
	defer rows.Close()
	var phoneNumber string
	var fullName sql.NullString
	var userName sql.NullString
	var customData sql.NullString
	var updatedAt time.Time
	for rows.Next() {
		err := rows.Scan(&fullName, &phoneNumber, &userName, &customData, &updatedAt)

		if err != nil {
			fmt.Println("GetProfileError: " + err.Error())
			return nil, err
		}
	}

	return &GetProfileResponse{
		Name:        fullName.String,
		PhoneNumber: phoneNumber,
		UserName:    userName.String,
		CustomData:  customData.String,
	}, nil
}

func (req *PutContactRequest) PutContact(userID uuid.UUID) (*PutContactResponse, error) {
	rows, err := db.Query(`SELECT user_id FROM profile where phone_number=$1`, req.PhoneNumber)
	if err != nil {
		fmt.Println(err.Error())
		return nil, err
	}

	defer rows.Close()
	var peerID string
	for rows.Next() {
		err := rows.Scan(&peerID)
		if err != nil {
			fmt.Println(err.Error())
			return nil, err
		}
	}

	if len(peerID) == 0 {
		return &PutContactResponse{
			Status: PutContactStatus_ContactIsNotInTheSystem,
		}, nil
	}

	_, err = db.Exec(`INSERT INTO contacts (user_id, chat_id, chat_type, name, created_at, updated_at, notification) values
											($1, $2, 0, $3, now(), now(), 0)
					  ON CONFLICT (user_id, chat_id) DO UPDATE SET updated_at=now(), name=$3
											`,
		userID.String(), peerID, req.ContactData.Name)
	if err != nil {
		return nil, err
	}

	return &PutContactResponse{
		Status: PutContactStatus_Success,
	}, nil

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

func (req *CreateGroupConversationRequest) CreateGroupConversation(userID uuid.UUID) (*CreateGroupConversationResponse, error) {
	ctx := context.Background()

	chatID := uuid.Must(uuid.NewV4(), nil)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		log.Println(err)
	}

	_, execErr := tx.Exec(`INSERT INTO group_list (chat_id, creator_id, created_at, updated_at, title) values ($1, $2, now(), now(), $3)`,
		chatID.String(), userID.String(), req.Name)
	if execErr != nil {
		_ = tx.Rollback()

		log.Println(execErr)
		return nil, errors.New("error-creating-group-table")
	}
	_, execErr = tx.Exec(`INSERT INTO chat_list (user_id, chat_id, created_at, updated_at, chat_type) values ($1, $2, now(), now(), 1)`, userID.String(), chatID.String())
	if execErr != nil {
		_ = tx.Rollback()

		log.Println(execErr)
		return nil, errors.New("error-creating-group-when-inserting-admin")
	}

	for _, participant := range req.Participants {

		_, execErr := tx.Exec(`INSERT INTO chat_list (user_id, chat_id, created_at, updated_at, chat_type) values ($1, $2, now(), now(), 1)`, participant.UserID, chatID.String())
		if execErr != nil {
			_ = tx.Rollback()

			log.Println(execErr)
			return nil, errors.New("error-creating-group-when-inserting-participant")
		}
	}

	if err := tx.Commit(); err != nil {
		log.Println(err)
	}

	return &CreateGroupConversationResponse{
		GroupID: chatID.String(),
	}, nil
}

func (req *VerifyOTPRequest) VerifyOTP() (*VerifyOTPResponse, error) {
	log.Println("Verifying OTP: " + req.PhoneNumber)

	uidRows, err := db.Query(`SELECT user_id FROM profile where phone_number=$1`, req.PhoneNumber)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	defer uidRows.Close()
	var userID string
	for uidRows.Next() {
		err := uidRows.Scan(&userID)
		if err != nil {
			log.Println(err)
			return nil, err
		}
	}

	if userID == "" {
		return nil, errors.New("verification-otp-no-user-found")
	}

	devRows, err := db.Query(`SELECT device_id FROM devices where user_id=$1 AND device_id=$2`, userID, req.DeviceID)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	defer devRows.Close()
	var deviceID string
	for devRows.Next() {
		err := devRows.Scan(&deviceID)
		if err != nil {
			log.Println(err)
			return nil, err
		}
	}

	if userID == "" {
		return nil, errors.New("verification-otp-no-device-found")
	}

	otpHash := xxhash.Sum64String(req.PhoneNumber+req.OTP) &^ (1 << 63)
	rows, err := db.Query(`DELETE FROM otp WHERE otp_code=$1 AND expired_at > now() RETURNING otp_code`, int64(otpHash))
	if err != nil {
		log.Println(err)
		print("Omama")
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var dbOTPHash uint64

		if err := rows.Scan(&dbOTPHash); err != nil {
			log.Println(err)
			return nil, err
		}

		if dbOTPHash == otpHash {

			_, err := db.Exec(`UPDATE devices set updated_at = now(), device_state = 1 WHERE device_id=$1 AND user_id=$2`, deviceID, userID)
			if err != nil {
				log.Println(err)
				return nil, err
			}

			b := make([]byte, 8)
			binary.LittleEndian.PutUint64(b, uint64(time.Now().UnixNano()))
			code := hex.EncodeToString(b)

			binary.LittleEndian.PutUint64(b, otpHash)
			code += hex.EncodeToString(b)

			err = redisClient.Set("DEV-"+code, deviceID, 0).Err()
			if err != nil {
				log.Println(err)
			}

			err = redisClient.Set("UID-"+code, userID, 0).Err()
			if err != nil {
				log.Println(err)
			}

			return &VerifyOTPResponse{Token: code}, nil
		}
	}

	return nil, errors.New("verification-otp-failed")
}
