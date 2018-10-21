// package ngobrel provides conversations records
package ngobrel

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"time"

	"github.com/minio/minio-go"

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
	SELECT b.is_admin, b.chat_type, b.excerpt, a.chat_id, a.title as chat_name, a.avatar_thumbnail as avatar_thumbnail, b.updated_at FROM group_list a, chat_list b WHERE a.chat_id = b.chat_id and b.user_id=$1
	UNION ALL
	SELECT 
		b.is_admin, 
		b.chat_type, 
		b.excerpt, 
		a.chat_id, 
		a.name as chat_name, 
		c.avatar_thumbnail as avatar_thumbnail, 
		b.updated_at 
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

		//var notification int64
		var updatedAt time.Time

		if err := rows.Scan(&isAdmin, &chatType, &excerpt, &chatID,
			&chatName, &avatarThumbnail,
			&updatedAt); err != nil {
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
		}
		list = append(list, item)
	}
	result := &ListConversationsResponse{
		List: list,
	}

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
	log.Println("PutContact " + userID.String())

	rows, err := db.Query(`SELECT user_id FROM profile where phone_number=$1`, req.PhoneNumber)
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

	_, err = db.Exec(`INSERT INTO contacts (user_id, chat_id, chat_type, name, created_at, updated_at, notification) values
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

func (req *GetContactsRequest) GetContacts(userID uuid.UUID) (*GetContactsResponse, error) {
	rows, err := db.Query(`
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

func (req *ListGroupParticipantsRequest) ListGroupParticipants(userID uuid.UUID) (*ListGroupParticipantsResponse, error) {

	var foundGroupID string
	foundRow, err := db.Query(`SELECT chat_id FROM chat_list where user_id=$1 AND chat_id=$2`, userID, req.GroupID)
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

	membersRow, err := db.Query(`
	SELECT p.user_id, g.is_admin, p.name, p.user_name, p.custom_data, p.phone_number
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
		var phoneNumber string
		if err := membersRow.Scan(&memberID, &isAdmin, &memberName, &userName, &customData, &phoneNumber); err != nil {
			log.Println(err)
			return nil, err
		}

		participant := &GroupParticipant{
			UserID:      memberID,
			IsAdmin:     (isAdmin == 1),
			Name:        memberName.String,
			UserName:    userName.String,
			CustomData:  customData.String,
			PhoneNumber: phoneNumber,
		}
		list = append(list, participant)
	}

	return &ListGroupParticipantsResponse{Participants: list}, nil
}

func isGroupAdmin(userID, groupID string) (bool, error) {
	var foundGroupID string
	foundRow, err := db.Query(`SELECT chat_id FROM chat_list where is_admin=1 AND user_id=$1 AND chat_id=$2`, userID, groupID)
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

func (req *RemoveAdminRoleRequest) RemoveAdminRole(userID uuid.UUID) (*RemoveAdminRoleResponse, error) {
	log.Println("Remove admin role")

	isGroupAdmin, err := isGroupAdmin(userID.String(), req.GroupID)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if isGroupAdmin == false {
		err := errors.New("not-an-admin")
		return nil, err
	}

	result, err := db.Exec(`UPDATE chat_list SET is_admin=0 WHERE is_admin=1 AND user_id=$1 AND chat_id=$2`, req.UserID, req.GroupID)
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

func (req *RemoveFromGroupRequest) RemoveFromGroup(userID uuid.UUID) (*RemoveFromGroupResponse, error) {
	isGroupAdmin, err := isGroupAdmin(userID.String(), req.GroupID)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if isGroupAdmin == false {
		err := errors.New("not-an-admin")
		return nil, err
	}

	result, err := db.Exec(`DELETE FROM chat_list WHERE user_id=$1 AND chat_id=$2`, req.UserID, req.GroupID)
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

func (req *ExitFromGroupRequest) ExitFromGroup(userID uuid.UUID) (*ExitFromGroupResponse, error) {
	result, err := db.Exec(`DELETE FROM chat_list WHERE user_id=$1 AND chat_id=$2`, userID, req.GroupID)
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

func (req *RenameGroupRequest) RenameGroup(userID uuid.UUID) (*RenameGroupResponse, error) {
	isGroupAdmin, err := isGroupAdmin(userID.String(), req.GroupID)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if isGroupAdmin == false {
		err := errors.New("not-an-admin")
		return nil, err
	}

	result, err := db.Exec(`UPDATE group_list SET title=$1 WHERE chat_id=$2`, req.NewName, req.GroupID)
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

func (req *AddToGroupRequest) AddToGroup(userID uuid.UUID) (*AddToGroupResponse, error) {
	isGroupAdmin, err := isGroupAdmin(userID.String(), req.GroupID)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if isGroupAdmin == false {
		err := errors.New("not-an-admin")
		return nil, err
	}

	ctx := context.Background()

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
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

func uploadMedia(userID uuid.UUID, mediaID string, isEncrypted bool, fileName, contentType string, fileSize int) error {
	_, err := db.Exec(`INSERT INTO media 
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

func uploadProfilePicture(userID uuid.UUID, mediaID string, thumbnail []byte, fileSize int) error {
	err := uploadMedia(userID, mediaID, false, "avatar", "application/png", fileSize)
	if err != nil {
		log.Println(err)
		return err
	}
	_, err = db.Exec(`UPDATE profile set avatar=$1, updated_at=now(), avatar_thumbnail=$2 WHERE user_id=$3`, mediaID, thumbnail, userID.String())

	if err != nil {
		log.Println(err)
		return err
	}

	return nil
}

func (req *GetProfilePictureRequest) getProfilePictureStream(srv *Server, userID uuid.UUID, stream Ngobrel_GetProfilePictureServer) error {

	log.Println("Get profile picture")
	rows, err := db.Query(`SELECT avatar FROM profile where user_id=$1`, req.UserID)
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
