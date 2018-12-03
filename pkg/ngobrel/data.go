// package ngobrel provides conversations records
package ngobrel

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go"

	"github.com/cespare/xxhash"
	"github.com/go-redis/redis"

	"os"

	_ "github.com/lib/pq"
	uuid "github.com/satori/go.uuid"
)

func (srv *Server) InitDB() {
	connStr := os.Getenv("DB_URL")
	redisStr := os.Getenv("REDIS_URL")
	fmt.Println("Connecting to DB " + connStr + " and redis: " + redisStr)
	srv.db, _ = sql.Open("postgres", connStr)

	srv.redisClient = redis.NewClient(&redis.Options{
		Addr:     redisStr,
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	pong, err := srv.redisClient.Ping().Result()
	log.Println(pong, err)
}

func getUserIDFromToken(srv *Server, token string) (string, error) {
	val, err := srv.redisClient.Get("UID-" + token).Result()
	if err != nil || (err == nil && val == "") {
		log.Println(err)

		err = errors.New("invalid-session")
		return "", err
	}
	return val, nil
}

func getDeviceIDFromToken(srv *Server, token string) (string, error) {
	val, err := srv.redisClient.Get("DEV-" + token).Result()
	if err != nil || (err == nil && val == "") {
		log.Println(err)
		err = errors.New("invalid-session")
		return "", err
	}
	return val, nil
}

func (req *PutMessageRequest) putMessageToUserIDCheckGroup(srv *Server, senderID uuid.UUID, senderDeviceID uuid.UUID, recipientID uuid.UUID, now float64) error {
	rows, err := srv.db.Query(`SELECT chat_id FROM group_list WHERE chat_id=$1`, recipientID.String())
	if err != nil {
		fmt.Println("err: " + err.Error())
		return err
	}

	defer rows.Close()
	ctx := context.Background()
	tx, err := srv.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})

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
	rows, err := srv.db.Query(`SELECT user_id FROM chat_list WHERE chat_id=$1`, chatID.String())
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
		rows, err := srv.db.Query(`SELECT device_id FROM devices WHERE user_id=$1 AND device_state = 1`, recipientID.String())
		if err != nil {
			log.Println(err)
			return err
		}
		defer rows.Close()
		found := false
		for rows.Next() {
			var deviceID uuid.UUID
			if err := rows.Scan(&deviceID); err != nil {
				log.Println(err)
				return err
			}

			err = req.putMessageToDeviceID(srv, tx, senderID, senderDeviceID, deviceID, now)
			if err != nil {
				log.Println(err)
				return err
			}
			found = true
		}
		if found && isGroup == false && req.MessageType == 0 {
			time.Sleep(100 * time.Millisecond)
			log.Println("Updating chat_list")
			_, err = tx.Exec(`
			INSERT INTO chat_list  (user_id, chat_id, created_at, updated_at, excerpt) values ($3, $2, now(), now(), $1) ON CONFLICT (user_id, chat_id) DO UPDATE SET excerpt=$1, updated_at=now()`,
				req.MessageExcerpt, recipientID.String(), senderID.String())
			if err != nil {
				log.Println(err)
				return err
			}
			_, err = tx.Exec(`
			INSERT INTO chat_list  (user_id, chat_id, created_at, updated_at, excerpt) values ($3, $2, now(), now(), $1) ON CONFLICT (user_id, chat_id) DO UPDATE SET excerpt=$1, updated_at=now()`,
				req.MessageExcerpt, senderID.String(), recipientID.String())
			if err != nil {
				log.Println(err)
				return err
			}

		}
		if found == false {
			log.Println("No devices found for recipient ", recipientID.String())
		}
	} else {
		// XXX TODO Encrypted version
	}
	return nil
}

func getNameFromUserID(srv *Server, senderID string, recipientID string) (string, error) {

	rows, err := srv.db.Query(`
	SELECT 
		'' as chat_name,
		name as profile_name, 
phone_number,
		user_name,
		custom_data
		FROM profile  WHERE user_id = $1
UNION 
SELECT 
name as chat_name,
'' as profile_name,                
'' as phone_number,
'' as user_name,
'' as custom_data
		FROM contacts  WHERE chat_id = $1 AND user_id = $2

	`, senderID, recipientID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	altName := ""
	name := ""

	for rows.Next() {
		var chatName sql.NullString
		var profileName sql.NullString
		var phoneNumber sql.NullString
		var userName sql.NullString
		var customData sql.NullString

		if err := rows.Scan(&chatName, &profileName,
			&phoneNumber,
			&userName,
			&customData); err != nil {

			log.Println(err)
			return "", err
		}

		if chatName.String != "" {
			name = chatName.String
		}

		if profileName.String != "" {
			name = profileName.String
		}

		if phoneNumber.String != "" {
			altName = phoneNumber.String
		}
	}
	if name == "" {
		name = altName
	}

	return name, nil
}

func (req *PutMessageRequest) putMessageToDeviceID(srv *Server, tx *sql.Tx, senderID uuid.UUID, senderDeviceID uuid.UUID, recipientDeviceID uuid.UUID, now float64) error {

	time.Sleep(100 * time.Millisecond)
	log.Println("putMessageToDeviceID: ", senderID.String(), req.MessageID, recipientDeviceID.String(), req.RecipientID)
	_, err := tx.Exec(`INSERT INTO conversations values ($1, $2, $3, $4, $5, to_timestamp($6), $7, $8)`,
		req.RecipientID, req.MessageID,
		senderID.String(), senderDeviceID.String(), recipientDeviceID.String(),
		now, req.MessageContents, req.MessageEncrypted)

	if err != nil {
		log.Println(req.MessageID)
		log.Println(err)
		return err
	}

	log.Println("Sending FCM notification")
	senderName, _ := getNameFromUserID(srv, senderID.String(), req.RecipientID)
	log.Println("--->", senderName, req.MessageExcerpt)
	ts := time.Now().UnixNano() / 1000
	srv.sendFCM(senderID.String(), senderName, req.RecipientID, req.MessageExcerpt, ts, req.MessageType == 1)
	/*
		data, ok := srv.receiptStream.Load(recipientDeviceID.String())
		if ok && data != nil {
			stream := data.(Ngobrel_GetMessageNotificationServer)
			fmt.Println("Ping " + recipientDeviceID.String())
			now := time.Now().UnixNano() / 1000
			stream.Send(&GetMessageNotificationStream{Timestamp: now, Sender: senderID.String(), Recipient: req.RecipientID})
			key := fmt.Sprintf("NOTIFICATION-%s%s-%d", senderID.String(), req.RecipientID, now)
			redisClient.Set(key, "1", 24*time.Hour)

			// 1. NotificationStream is sent to the client, a redis key is set
			// 2.a. In active mode, client calls AckNotification, which removes the key
			// 2.b. otherwise, the key stays until it expires
			// 3. The key is checked here, if it exists, an FCM notification is sent
			timer := time.NewTimer(time.Second * 1)
			go func() {
				<-timer.C
				key = fmt.Sprintf("NOTIFICATION-%s%s-%d", senderID.String(), req.RecipientID, now)
				val, err := redisClient.Get(key).Result()
				if err != nil {
					log.Println("Notification already cleared")
					return
				}
				log.Println("Sending FCM notification")
				if val != "" {
					senderName, err := getNameFromUserID(senderID.String(), req.RecipientID)
					log.Println("--->", val, "--->", senderName)

					if err != nil {
						log.Println(err)
					}
					srv.sendFCM(senderID.String(), senderName, req.RecipientID, req.MessageExcerpt, now)
				}
			}()
		} else {
			return errors.New("invalid-stream")
		}
	*/

	return nil
}

func (req *GetMessagesRequest) getMessageNotificationStream(srv *Server, recipientDeviceID uuid.UUID, stream Ngobrel_GetMessageNotificationServer) error {
	// subscribe
	p := fmt.Sprintf("%p", stream)
	lastP := p
	srv.receiptStream.Store(recipientDeviceID.String(), stream)
	log.Println(recipientDeviceID.String() + " is susbscribed")
	for {
		s, _ := srv.receiptStream.Load(recipientDeviceID.String())
		if s == nil {
			log.Println("Discarding empty stream")
			break
		}
		p := fmt.Sprintf("%p", s)
		if p != lastP {
			log.Println("Discarding dangled stream")
			break
		}
		// suspend
		fmt.Println("Notification stream for " + recipientDeviceID.String())
		time.Sleep(5 * 60 * time.Second)
	}
	return nil
}

func (req *GetMessagesRequest) getMessages(srv *Server, recipientDeviceID uuid.UUID, stream Ngobrel_GetMessagesServer) error {

	fmt.Println("Getting messages for device id" + recipientDeviceID.String())
	rows, err := srv.db.Query(`DELETE FROM conversations WHERE recipient_device_id=$1 RETURNING recipient_id, message_id, sender_id, 
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

func (req *CreateConversationRequest) CreateConversation(srv *Server, userID uuid.UUID) (*CreateConversationResponse, error) {
	_, err := srv.db.Exec(`INSERT INTO chat_list values ($1, $2, $3, now(), now(), $4)`,
		userID.String(), req.ChatID, req.Type, 0)

	if err != nil {
		return nil, err
	}
	return &CreateConversationResponse{ChatID: req.ChatID, Message: ""}, nil
}

func (req *UpdateConversationRequest) UpdateConversation(srv *Server, userID uuid.UUID) (*UpdateConversationResponse, error) {
	_, err := srv.db.Exec(`
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

func (req *ListConversationsRequest) ListConversations(srv *Server, userID uuid.UUID) (*ListConversationsResponse, error) {
	fmt.Println(userID.String())

	rows, err := srv.db.Query(`
	SELECT b.is_admin, 
		b.chat_type,
		b.excerpt,
		a.chat_id,
		a.title as chat_name,
		a.avatar_thumbnail as avatar_thumbnail,
		b.updated_at,
		'','',''
		FROM group_list a, chat_list b WHERE a.chat_id = b.chat_id and b.user_id=$1
	UNION ALL
	SELECT 
		b.is_admin, 
		b.chat_type, 
		b.excerpt, 
		a.chat_id, 
		a.name as chat_name, 
		c.avatar_thumbnail as avatar_thumbnail, 
		b.updated_at,
		c.phone_number,
		c.user_name,
		c.custom_data
		FROM contacts a, chat_list b, profile c WHERE a.chat_id = b.chat_id and a.user_id = b.user_id and c.user_id=b.chat_id and b.user_id=$1
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
		var isAdmin int32
		var avatarThumbnail []byte
		var phoneNumber sql.NullString
		var userName sql.NullString
		var customData sql.NullString

		//var notification int64
		var updatedAt time.Time

		if err := rows.Scan(&isAdmin, &chatType, &excerpt, &chatID,
			&chatName, &avatarThumbnail,
			&updatedAt,
			&phoneNumber,
			&userName,
			&customData); err != nil {
			return nil, err
		}

		item := &Conversations{
			ChatID:    chatID.String(),
			Timestamp: updatedAt.UnixNano() / 1000000,
			//Notification: int64(notification),
			ChatName:        chatName,
			Excerpt:         excerpt,
			ChatType:        chatType,
			IsGroupAdmin:    isAdmin == 1,
			AvatarThumbnail: avatarThumbnail,
			PhoneNumber:     phoneNumber.String,
			UserName:        userName.String,
			CustomData:      customData.String,
		}
		list = append(list, item)
	}
	result := &ListConversationsResponse{
		List: list,
	}

	return result, nil
}

func (req *CreateProfileRequest) CreateProfile(srv *Server) (*CreateProfileResponse, error) {
	rows, err := srv.db.Query(`SELECT user_id FROM profile where phone_number=$1`, req.PhoneNumber)
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

	ctx := context.Background()

	tx, err := srv.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		log.Println(err)
	}

	if len(userID) == 0 {
		newUUID := uuid.Must(uuid.NewV4(), nil)
		userID = newUUID.String()
		_, err = tx.Exec(`INSERT INTO profile (user_id, phone_number, created_at, updated_at) values ($1, $2, now(), now());`, userID, req.PhoneNumber)
		if err != nil {
			_ = tx.Rollback()
			fmt.Println(err.Error())
			return nil, err
		}
	}

	fmt.Println(userID + ":" + req.DeviceID + ":" + req.PhoneNumber)
	_, err = tx.Exec(`UPDATE devices set updated_at = now(), device_state = 0 WHERE device_id != $1 AND user_id=$2`, req.DeviceID, userID)
	if err != nil {
		_ = tx.Rollback()
		log.Println(err)
		return nil, err
	}

	_, err = tx.Exec(`INSERT INTO devices (user_id, device_id, updated_at, created_at, device_state) values ($1, $2, now(), now(), 0)
	ON CONFLICT (user_id, device_id) DO UPDATE SET device_state=0, user_id=$1, device_id=$2, updated_at=now() 
	`, userID, req.DeviceID)
	if err != nil {
		_ = tx.Rollback()
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
	_, err = tx.Exec(`INSERT INTO otp (otp_code, expired_at) values ($1, now() + interval '1 day')`, int64(otpHash))
	if err != nil {
		_ = tx.Rollback()
		fmt.Println(err.Error())
		return nil, err
	}

	if DebugMode == false {
		otpCode = ""
	}

	if err := tx.Commit(); err != nil {
		log.Println(err)
	}

	return &CreateProfileResponse{
		UserID:   userID,
		OtpDebug: otpCode, // DEBUG Mode
	}, nil
}

func (req *EditProfileRequest) EditProfile(srv *Server, userID uuid.UUID) (*EditProfileResponse, error) {
	result, err := srv.db.Exec(`UPDATE profile set user_name=$1, name=$2, custom_data=$3, updated_at=now() WHERE user_id=$4;`,
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

func (req *GetProfileRequest) GetProfile(srv *Server, userID uuid.UUID) (*GetProfileResponse, error) {
	qUserID := userID.String()
	if req.UserID != "" {
		qUserID = req.UserID
	}
	rows, err := srv.db.Query(`SELECT name, phone_number, user_name, custom_data, updated_at from profile WHERE user_id=$1;`,
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

func (req *PutContactRequest) PutContact(srv *Server, userID uuid.UUID) (*PutContactResponse, error) {
	log.Println("PutContact " + userID.String())

	rows, err := srv.db.Query(`SELECT user_id FROM profile where phone_number=$1`, req.PhoneNumber)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	defer rows.Close()
	var peerID string
	for rows.Next() {
		err := rows.Scan(&peerID)
		if err != nil {
			log.Println(err)
			return nil, err
		}
	}
	if len(peerID) == 0 {
		err := errors.New("target-is-not-in-the-system")
		log.Println(err)
		return nil, err
	}

	_, err = srv.db.Exec(`INSERT INTO contacts (user_id, chat_id, chat_type, name, created_at, updated_at, notification) values
											($1, $2, 0, $3, now(), now(), 0)
					  ON CONFLICT (user_id, chat_id) DO UPDATE SET updated_at=now(), name=$3
											`,
		userID.String(), peerID, req.ContactData.Name)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return &PutContactResponse{
		Success: true,
	}, nil

}

func (req *GetContactsRequest) GetContacts(srv *Server, userID uuid.UUID) (*GetContactsResponse, error) {
	rows, err := srv.db.Query(`
		SELECT a.chat_id as chat_id, 
			a.name as name, 
			a.updated_at as updated_at, 
			b.avatar_thumbnail as avatar_thumbnail, 
			a.notification as notification,
			b.phone_number as phone_number,
			b.user_name as user_name,
			b.custom_data as custom_data
	FROM contacts a, profile b
	WHERE a.user_id=$1 
	AND a.chat_id=b.user_id
	AND a.chat_type=0 ORDER BY name
	`, userID.String())
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
		var avatarThumbnail []byte
		var phoneNumber string
		var userName sql.NullString
		var customData sql.NullString

		if err := rows.Scan(&peerID,
			&chatName,
			&updatedAt,
			&avatarThumbnail,
			&notification,
			&phoneNumber,
			&userName,
			&customData,
		); err != nil {
			return nil, err
		}

		item := &Contacts{
			PeerID:          peerID.String(),
			Name:            chatName,
			AvatarThumbnail: avatarThumbnail,
			Notification:    int64(notification),
			PhoneNumber:     phoneNumber,
			UserName:        userName.String,
			CustomData:      customData.String,
		}

		fmt.Println(item)
		list = append(list, item)
	}
	result := &GetContactsResponse{
		List: list,
	}
	return result, nil
}

func (req *CreateGroupConversationRequest) CreateGroupConversation(srv *Server, userID uuid.UUID) (*CreateGroupConversationResponse, error) {
	ctx := context.Background()

	chatID := uuid.Must(uuid.NewV4(), nil)
	tx, err := srv.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
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
	_, execErr = tx.Exec(`INSERT INTO chat_list (user_id, chat_id, created_at, updated_at, chat_type, is_admin) values ($1, $2, now(), now(), 1, 1)`, userID.String(), chatID.String())
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

func (req *VerifyOTPRequest) VerifyOTP(srv *Server) (*VerifyOTPResponse, error) {
	log.Println("Verifying OTP: " + req.PhoneNumber)

	uidRows, err := srv.db.Query(`SELECT user_id FROM profile where phone_number=$1`, req.PhoneNumber)
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
		log.Println("No user found by number ", req.PhoneNumber)
		return nil, errors.New("verification-otp-no-user-found")
	}

	devRows, err := srv.db.Query(`SELECT device_id FROM devices where user_id=$1 AND device_id=$2`, userID, req.DeviceID)
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
	rows, err := srv.db.Query(`DELETE FROM otp WHERE otp_code=$1 AND expired_at > now() RETURNING otp_code`, int64(otpHash))
	if err != nil {
		log.Println(err)
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
			log.Println("Verified")

			_, err := srv.db.Exec(`UPDATE devices set updated_at = now(), device_state = 1 WHERE device_id=$1 AND user_id=$2`, deviceID, userID)
			if err != nil {
				log.Println(err)
				return nil, err
			}

			b := make([]byte, 8)
			binary.LittleEndian.PutUint64(b, uint64(time.Now().UnixNano()))
			code := hex.EncodeToString(b)

			binary.LittleEndian.PutUint64(b, otpHash)
			code += hex.EncodeToString(b)

			err = srv.redisClient.Set("DEV-"+code, deviceID, 0).Err()
			if err != nil {
				log.Println(err)
			}

			err = srv.redisClient.Set("UID-"+code, userID, 0).Err()
			if err != nil {
				log.Println(err)
			}

			return &VerifyOTPResponse{Token: code}, nil
		}
	}

	log.Println("Verification failed for", req.PhoneNumber)
	return nil, errors.New("verification-otp-failed")
}

func (req *ListGroupParticipantsRequest) ListGroupParticipants(srv *Server, userID uuid.UUID) (*ListGroupParticipantsResponse, error) {

	var foundGroupID string
	foundRow, err := srv.db.Query(`SELECT chat_id FROM chat_list where user_id=$1 AND chat_id=$2`, userID, req.GroupID)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	defer foundRow.Close()
	for foundRow.Next() {

		if err := foundRow.Scan(&foundGroupID); err != nil {
			log.Println(err)
			return nil, err
		}
	}

	if foundGroupID != req.GroupID {
		err := errors.New("group-not-found")
		log.Println(err)
		return nil, err
	}

	membersRow, err := srv.db.Query(`
	SELECT p.user_id, g.is_admin, p.name, p.user_name, p.custom_data, p.phone_number, p.avatar, p.avatar_thumbnail
	FROM chat_list g, profile p
	WHERE 
	g.user_id=p.user_id AND
	chat_id=$1`, req.GroupID)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	defer membersRow.Close()

	var list []*GroupParticipant = []*GroupParticipant{}

	for membersRow.Next() {
		var memberID string
		var isAdmin int
		var memberName sql.NullString
		var userName sql.NullString
		var customData sql.NullString
		var avatar sql.NullString
		var avatarThumbnail []byte
		var phoneNumber string
		if err := membersRow.Scan(&memberID,
			&isAdmin,
			&memberName,
			&userName,
			&customData,
			&phoneNumber,
			&avatar,
			&avatarThumbnail); err != nil {
			log.Println(err)
			return nil, err
		}

		participant := &GroupParticipant{
			UserID:          memberID,
			IsAdmin:         (isAdmin == 1),
			Name:            memberName.String,
			UserName:        userName.String,
			CustomData:      customData.String,
			PhoneNumber:     phoneNumber,
			Avatar:          avatar.String,
			AvatarThumbnail: avatarThumbnail,
		}
		list = append(list, participant)
	}

	return &ListGroupParticipantsResponse{Participants: list}, nil
}

func isGroupAdmin(srv *Server, userID, groupID string) (bool, error) {
	var foundGroupID string
	foundRow, err := srv.db.Query(`SELECT chat_id FROM chat_list where is_admin=1 AND user_id=$1 AND chat_id=$2`, userID, groupID)
	if err != nil {
		log.Println(err)
		return false, err
	}

	defer foundRow.Close()
	for foundRow.Next() {
		if err := foundRow.Scan(&foundGroupID); err != nil {
			log.Println(err)
			return false, err
		}
	}

	return foundGroupID == groupID, nil
}

func (req *RemoveAdminRoleRequest) RemoveAdminRole(srv *Server, userID uuid.UUID) (*RemoveAdminRoleResponse, error) {
	log.Println("Remove admin role")

	isGroupAdmin, err := isGroupAdmin(srv, userID.String(), req.GroupID)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if isGroupAdmin == false {
		err := errors.New("not-an-admin")
		return nil, err
	}

	result, err := srv.db.Exec(`UPDATE chat_list SET is_admin=0 WHERE is_admin=1 AND user_id=$1 AND chat_id=$2`, req.UserID, req.GroupID)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	count, err := result.RowsAffected()

	if err != nil {
		log.Println(err)
		return nil, err
	}

	success := false
	if count == 1 {
		success = true
	}

	return &RemoveAdminRoleResponse{Success: success}, nil
}

func (req *RemoveFromGroupRequest) RemoveFromGroup(srv *Server, userID uuid.UUID) (*RemoveFromGroupResponse, error) {
	isGroupAdmin, err := isGroupAdmin(srv, userID.String(), req.GroupID)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if isGroupAdmin == false {
		err := errors.New("not-an-admin")
		return nil, err
	}

	result, err := srv.db.Exec(`DELETE FROM chat_list WHERE user_id=$1 AND chat_id=$2`, req.UserID, req.GroupID)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	count, err := result.RowsAffected()
	if err != nil {
		log.Println(err)
		return nil, err
	}

	success := false
	if count == 1 {
		success = true
	}

	return &RemoveFromGroupResponse{Success: success}, nil
}

func (req *ExitFromGroupRequest) ExitFromGroup(srv *Server, userID uuid.UUID) (*ExitFromGroupResponse, error) {
	result, err := srv.db.Exec(`DELETE FROM chat_list WHERE user_id=$1 AND chat_id=$2`, userID, req.GroupID)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	count, err := result.RowsAffected()
	if err != nil {
		log.Println(err)
		return nil, err
	}

	success := false
	if count == 1 {
		success = true
	}

	return &ExitFromGroupResponse{Success: success}, nil
}

func (req *RenameGroupRequest) RenameGroup(srv *Server, userID uuid.UUID) (*RenameGroupResponse, error) {
	isGroupAdmin, err := isGroupAdmin(srv, userID.String(), req.GroupID)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if isGroupAdmin == false {
		err := errors.New("not-an-admin")
		return nil, err
	}

	result, err := srv.db.Exec(`UPDATE group_list SET title=$1 WHERE chat_id=$2`, req.NewName, req.GroupID)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	count, err := result.RowsAffected()
	if err != nil {
		log.Println(err)
		return nil, err
	}

	success := false
	if count == 1 {
		success = true
	}

	return &RenameGroupResponse{Success: success}, nil
}

func (req *AddToGroupRequest) AddToGroup(srv *Server, userID uuid.UUID) (*AddToGroupResponse, error) {
	isGroupAdmin, err := isGroupAdmin(srv, userID.String(), req.GroupID)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if isGroupAdmin == false {
		err := errors.New("not-an-admin")
		return nil, err
	}

	ctx := context.Background()

	tx, err := srv.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		log.Println(err)
	}

	for _, participant := range req.Participants {
		_, execErr := tx.Exec(`INSERT INTO chat_list (user_id, chat_id, created_at, updated_at, chat_type) values ($1, $2, now(), now(), 1)`, participant.UserID, req.GroupID)
		if execErr != nil {
			_ = tx.Rollback()

			log.Println(execErr)
			return nil, errors.New("error-add-group-when-inserting-participant")
		}
	}

	if err := tx.Commit(); err != nil {
		log.Println(err)
	}

	return &AddToGroupResponse{
		GroupID: req.GroupID,
	}, nil
}

func uploadMedia(srv *Server, userID uuid.UUID, mediaID string, isEncrypted bool, fileName, contentType string, fileSize int) error {
	_, err := srv.db.Exec(`INSERT INTO media 
		(uploader, file_id, created_at, is_encrypted, file_name, content_type, file_size)
		values
		($1, $2, now(), $3, $4, $5, $6)
		`, userID.String(), mediaID, isEncrypted, fileName, contentType, fileSize)
	if err != nil {
		log.Println(err)
		return err
	}

	return nil
}

func uploadProfilePicture(srv *Server, userID uuid.UUID, mediaID string, thumbnail []byte, fileSize int) error {
	err := uploadMedia(srv, userID, mediaID, false, "avatar", "application/png", fileSize)
	if err != nil {
		log.Println(err)
		return err
	}
	_, err = srv.db.Exec(`UPDATE profile set avatar=$1, updated_at=now(), avatar_thumbnail=$2 WHERE user_id=$3`, mediaID, thumbnail, userID.String())

	if err != nil {
		log.Println(err)
		return err
	}

	return nil
}

func updateGroupAvatar(srv *Server, userID uuid.UUID, groupID, mediaID string, thumbnail []byte) error {
	_, err := srv.db.Exec(`UPDATE media set uploader=$1 WHERE uploader=$2 and file_id=$3`, groupID, userID.String(), mediaID)
	if err != nil {
		log.Println(err)
		return err
	}
	_, err = srv.db.Exec(`UPDATE group_list set avatar=$1, updated_at=now(), avatar_thumbnail=$2 WHERE chat_id=$3`, mediaID, thumbnail, groupID)

	if err != nil {
		log.Println(err)
		return err
	}

	return nil
}

func (req *GetMediaRequest) getMediaStream(srv *Server, userID uuid.UUID, stream Ngobrel_GetMediaServer) error {

	log.Println("Get media")
	rows, err := srv.db.Query(`SELECT uploader, file_id, is_encrypted, file_name, content_type, file_size FROM media where file_id=$1`, req.MediaID)
	if err != nil {
		log.Println(err)
		return err
	}

	defer rows.Close()
	var uploader sql.NullString
	var fileID sql.NullString
	var fileName sql.NullString
	var contentType sql.NullString
	var fileSize int
	var isEncrypted bool
	for rows.Next() {
		if err := rows.Scan(&uploader, &fileID, &isEncrypted, &fileName, &contentType, &fileSize); err != nil {
			log.Println(err)
			return err
		}
	}

	if fileID.String == "" {
		err := errors.New("no-media-file")
		log.Println(err)
		return err
	}

	obj, err := srv.minioClient.GetObject(uploader.String, fileID.String, minio.GetObjectOptions{})
	if err != nil {
		log.Println(err)
		return err
	}

	defer obj.Close()
	buffer := make([]byte, 32*1024)
	for {
		n, err := obj.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}

			log.Println(err)
			return err
		}
		stream.Send(&GetMediaResponse{Contents: buffer[:n]})
	}
	return nil
}

func (req *GetProfilePictureRequest) getProfilePictureStream(srv *Server, userID uuid.UUID, stream Ngobrel_GetProfilePictureServer) error {

	log.Println("Get profile picture")
	rows, err := srv.db.Query(`SELECT avatar FROM profile where user_id=$1`, req.UserID)
	if err != nil {
		log.Println(err)
		return err
	}

	defer rows.Close()
	var fileID sql.NullString
	for rows.Next() {
		if err := rows.Scan(&fileID); err != nil {
			log.Println(err)
			return err
		}
	}

	if fileID.String == "" {
		err := errors.New("no-profile-picture")
		log.Println(err)
		return err
	}

	obj, err := srv.minioClient.GetObject(req.UserID, fileID.String, minio.GetObjectOptions{})
	if err != nil {
		log.Println(err)
		return err
	}

	defer obj.Close()
	buffer := make([]byte, 32*1024)
	for {
		n, err := obj.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}

			log.Println(err)
			return err
		}
		stream.Send(&GetProfilePictureResponse{Contents: buffer[:n]})
	}
	return nil
}

func (req *AckMessageNotificationStreamRequest) AckMessageNotificationStream(userID uuid.UUID) (*AckMessageNotificationStreamResponse, error) {
	if userID.String() != req.Recipient {
		err := errors.New("recipient-mismatch")
		log.Println(err, userID.String(), ":", req.Recipient)
		return nil, err
	}
	log.Println("Removing notification")

	//key := fmt.Sprintf("NOTIFICATION-%s%s-%d", req.Sender, req.Recipient, req.Timestamp)
	//err := redisClient.Del(key).Err()

	//if err != nil {
	//	log.Println(err)
	// ignore
	//}

	return &AckMessageNotificationStreamResponse{Success: true}, nil
}

func (req *PutMessageStateRequest) PutMessageState(srv *Server, userID uuid.UUID, senderDeviceID uuid.UUID, now float64) (*PutMessageStateResponse, error) {

	log.Println(fmt.Sprintf("PutMessageState %s %s %f", userID.String(), senderDeviceID.String(), now))

	contents, _ := json.Marshal(&ManagementMessage{
		MessageType: "management",
		Text:        "reception-receipt",
		Command: ManagementReceptionStateMessage{
			Type:      req.Status,
			MessageID: req.MessageID,
		},
	})

	for true {
		outgoingMessageID := (time.Now().UnixNano() / 1000000) - 946659600000 // 2000-01-01T00:00:00
		receipt := &PutMessageRequest{
			RecipientID:      req.ChatID,
			MessageID:        outgoingMessageID,
			MessageExcerpt:   "",
			MessageEncrypted: false,
			MessageContents:  string(contents),
			MessageType:      1, // management
		}

		chatID, err := uuid.FromString(req.ChatID)
		if err != nil {
			log.Println(err)
			return nil, err
		}

		err = receipt.putMessageToUserIDCheckGroup(srv, userID, senderDeviceID, chatID, now)
		if err != nil {
			if strings.Contains(err.Error(), "duplicate key value violates unique constraint \"conversations_pkey\"") {
				log.Println(err, " Try again")
				time.Sleep(100 * time.Millisecond)
				continue
			}
			if strings.Contains(err.Error(), "could not serialize access due to concurrent update") {
				log.Println(err, " Try again")
				time.Sleep(100 * time.Millisecond)
				continue
			}
			log.Println(err)
			return nil, err
		}
		break
	}

	return &PutMessageStateResponse{
		Success: true,
	}, nil
}
