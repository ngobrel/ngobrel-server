package ngobrel

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/disintegration/imaging"

	"github.com/cespare/xxhash"

	uuid "github.com/satori/go.uuid"

	"github.com/go-redis/redis"
	minio "github.com/minio/minio-go"

	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
)

type FCMAuth struct {
	client  *http.Client
	expired time.Time
}

type Server struct {
	receiptStream sync.Map
	smsClient     Sms
	minioClient   minio.Client
	tmpDir        string
	fcmAuth       FCMAuth
	db            *sql.DB
	redisClient   *redis.Client
}

type ManagementMessage struct {
	MessageType string                          `json:"messageType"`
	Text        string                          `json:"text"`
	Command     ManagementReceptionStateMessage `json:"command"`
}

type ManagementReceptionStateMessage struct {
	Type      MessageReceptionState `json:"type"`
	MessageID int64                 `json:"messageId"`
}

func NewServer(sms Sms, minioClient minio.Client) *Server {
	tmpDir := os.Getenv("TMPDIR")
	if tmpDir == "" {
		log.Println("TMPDIR environment variable is not set, using /tmp as temp directory.")
		tmpDir = "/tmp"
	}

	fcmConfigPath := os.Getenv("FCM_CONFIG_PATH")
	if fcmConfigPath == "" {
		log.Fatal("FCM_CONFIG_PATH is not set")
	}

	data, err := ioutil.ReadFile(fcmConfigPath)
	if err != nil {
		log.Fatal(err)
		return nil
	}
	googleConfig, err := google.JWTConfigFromJSON(data, "https://www.googleapis.com/auth/firebase.messaging")
	if err != nil {
		log.Fatal("Unable to login to Google Firebase")
	}

	fcmAuth := FCMAuth{
		client:  googleConfig.Client(oauth2.NoContext),
		expired: time.Now().Add(1 * time.Hour),
	}

	log.SetFlags(log.Lshortfile)
	return &Server{
		smsClient:   sms,
		minioClient: minioClient,
		tmpDir:      tmpDir,
		fcmAuth:     fcmAuth,
	}
}

func getID(ctx context.Context, id string) (uuid.UUID, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok == false {
		return uuid.Nil, errors.New("no-metadata-available")
	}

	idList := md.Get(id)
	if idList == nil || len(idList) == 0 || idList[0] == "" {
		return uuid.Nil, errors.New("no-id-available: " + id)
	}

	return uuid.FromString(idList[0])
}

func getToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok == false {
		return "", errors.New("no-metadata-available")
	}

	idList := md.Get("token")
	if idList == nil || len(idList) == 0 || idList[0] == "" {
		return "", errors.New("no-token-available")
	}

	return idList[0], nil
}

func getDeviceID(srv *Server, ctx context.Context) (uuid.UUID, error) {
	token, err := getToken(ctx)
	if err != nil {
		return uuid.Nil, err
	}

	id, err := getDeviceIDFromToken(srv, token)
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.FromString(id)
}

func getUserID(srv *Server, ctx context.Context) (uuid.UUID, error) {
	token, err := getToken(ctx)
	if err != nil {
		return uuid.Nil, err
	}

	id, err := getUserIDFromToken(srv, token)
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.FromString(id)
}

func (srv *Server) GetMessageNotification(in *GetMessagesRequest, stream Ngobrel_GetMessageNotificationServer) error {
	recipientDeviceID, err := getDeviceID(srv, stream.Context())
	if err != nil {
		return err
	}

	return in.getMessageNotificationStream(srv, recipientDeviceID, stream)
}

func (srv *Server) GetMessages(in *GetMessagesRequest, stream Ngobrel_GetMessagesServer) error {
	recipientDeviceID, err := getDeviceID(srv, stream.Context())
	if err != nil {
		return err
	}

	return in.getMessages(srv, recipientDeviceID, stream)
}

func (srv *Server) PutMessage(ctx context.Context, in *PutMessageRequest) (*PutMessageResponse, error) {

	senderID, err := getUserID(srv, ctx)
	if err != nil {
		return nil, err
	}
	senderDeviceID, err := getDeviceID(srv, ctx)
	if err != nil {
		return nil, err
	}

	recipientID, err := uuid.FromString(in.RecipientID)
	if err != nil {
		return nil, err
	}

	in.MessageID = (time.Now().UnixNano() / 1000000) - 946659600000 // 2000-01-01T00:00:00

	now := time.Now().UnixNano() / 1000.0 // in microsecs
	nowFloat := float64(now) / 1000000.0  // in secs

	err = in.putMessageToUserIDCheckGroup(srv, senderID, senderDeviceID, recipientID, nowFloat)
	if err != nil {
		return nil, err
	}
	return &PutMessageResponse{MessageID: int64(in.MessageID), MessageTimestamp: now}, nil
}

func (srv *Server) CreateConversation(ctx context.Context, in *CreateConversationRequest) (*CreateConversationResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		return nil, err
	}

	return in.CreateConversation(srv, userID)
}

func (srv *Server) ListConversations(ctx context.Context, in *ListConversationsRequest) (*ListConversationsResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		return nil, err
	}

	return in.ListConversations(srv, userID)
}

func (srv *Server) GetContacts(ctx context.Context, in *GetContactsRequest) (*GetContactsResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		return nil, err
	}

	return in.GetContacts(srv, userID)
}

func (srv *Server) UpdateConversation(ctx context.Context, in *UpdateConversationRequest) (*UpdateConversationResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		return nil, err
	}

	return in.UpdateConversation(srv, userID)
}

func (srv *Server) CreateProfile(ctx context.Context, in *CreateProfileRequest) (*CreateProfileResponse, error) {
	return in.CreateProfile(srv)
}

func (srv *Server) EditProfile(ctx context.Context, in *EditProfileRequest) (*EditProfileResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		return nil, err
	}
	return in.EditProfile(srv, userID)
}

func (srv *Server) GetProfile(ctx context.Context, in *GetProfileRequest) (*GetProfileResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		return nil, err
	}
	return in.GetProfile(srv, userID)
}

func (srv *Server) PutContact(ctx context.Context, in *PutContactRequest) (*PutContactResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		return nil, err
	}

	return in.PutContact(srv, userID)
}

func (srv *Server) CreateGroupConversation(ctx context.Context, in *CreateGroupConversationRequest) (*CreateGroupConversationResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		return nil, err
	}

	ret, err := in.CreateGroupConversation(srv, userID)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	err = srv.PrepareMediaForGroup(userID, ret.GroupID, in.Avatar)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	return ret, nil
}

func (srv *Server) VerifyOTP(ctx context.Context, in *VerifyOTPRequest) (*VerifyOTPResponse, error) {

	return in.VerifyOTP(srv)
}

func (srv *Server) ListGroupParticipants(ctx context.Context, in *ListGroupParticipantsRequest) (*ListGroupParticipantsResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		return nil, err
	}

	return in.ListGroupParticipants(srv, userID)
}

func (srv *Server) RemoveAdminRole(ctx context.Context, in *RemoveAdminRoleRequest) (*RemoveAdminRoleResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	if userID.String() == in.UserID {
		err := errors.New("remove-admin-role-cant-remove-self")
		return nil, err
	}

	return in.RemoveAdminRole(srv, userID)
}

func (srv *Server) RemoveFromGroup(ctx context.Context, in *RemoveFromGroupRequest) (*RemoveFromGroupResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	if userID.String() == in.UserID {
		err := errors.New("remove-from-group-cant-remove-self")
		return nil, err
	}

	return in.RemoveFromGroup(srv, userID)
}

func (srv *Server) AddToGroup(ctx context.Context, in *AddToGroupRequest) (*AddToGroupResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return in.AddToGroup(srv, userID)
}

func (srv *Server) ExitFromGroup(ctx context.Context, in *ExitFromGroupRequest) (*ExitFromGroupResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return in.ExitFromGroup(srv, userID)
}

func (srv *Server) RenameGroup(ctx context.Context, in *RenameGroupRequest) (*RenameGroupResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return in.RenameGroup(srv, userID)
}

func (srv *Server) Echo(ctx context.Context, in *EchoRequest) (*EchoResponse, error) {
	log.Println("Echo")
	return &EchoResponse{
		Reply: "Omama: [" + in.Message + "]",
	}, nil
}

func getRandomID() string {
	b1 := make([]byte, 8)
	binary.LittleEndian.PutUint64(b1, uint64(time.Now().UnixNano()))
	b2 := make([]byte, 8)
	binary.LittleEndian.PutUint64(b2, xxhash.Sum64(b1))
	mediaID := hex.EncodeToString(b2)

	return mediaID
}

func (srv *Server) UploadProfilePicture(stream Ngobrel_UploadProfilePictureServer) error {

	userID, err := getUserID(srv, stream.Context())
	if err != nil {
		log.Println(err)
		return err
	}

	mediaID := getRandomID()

	exists, err := srv.minioClient.BucketExists(userID.String())
	if err != nil {
		log.Println(err)
	}

	if exists == false {
		err = srv.minioClient.MakeBucket(userID.String(), "us-east-1")
		if err != nil {
			log.Println(err)
			return err
		}
	}

	tmpFileName := srv.tmpDir + "/" + userID.String() + "-" + mediaID
	log.Println(tmpFileName)
	incomingFile, err := os.Create(tmpFileName)
	if err != nil {
		log.Println(err)
		return err
	}
	defer incomingFile.Close()

	fileSize := 0
	for {
		data, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}

			log.Println(err)
			return err
		}

		size, err := incomingFile.Write(data.Contents)
		if err != nil {
			log.Println(err)
			return err
		}
		fileSize += size
	}

	err = incomingFile.Sync()
	if err != nil {
		log.Println(err)
		return err
	}

	incomingFile.Close()

	_, err = srv.minioClient.FPutObject(userID.String(), mediaID, tmpFileName, minio.PutObjectOptions{ContentType: "application/png"})
	if err != nil {
		log.Println(err)
	}

	src, err := imaging.Open(tmpFileName)
	if err != nil {
		log.Println("failed to open image", err)
		return err
	}
	src = imaging.Resize(src, 140, 140, imaging.Lanczos)
	var b bytes.Buffer
	imaging.Encode(&b, src, imaging.PNG)

	os.Remove(tmpFileName)
	err = uploadProfilePicture(srv, userID, mediaID, b.Bytes(), fileSize)
	if err != nil {
		log.Println(err)
		stream.SendAndClose(nil)
		return err
	}

	err = stream.SendAndClose(&UploadProfilePictureResponse{MediaID: mediaID})
	if err != nil {
		log.Println(err)
	}
	return err
}

func (srv *Server) UploadMedia(stream Ngobrel_UploadMediaServer) error {

	userID, err := getUserID(srv, stream.Context())
	if err != nil {
		log.Println(err)
		return err
	}

	mediaID := getRandomID()

	var fileName string
	var contentType string
	isEncrypted := false

	exists, err := srv.minioClient.BucketExists(userID.String())
	if err != nil {
		log.Println(err)
	}

	if exists == false {
		err = srv.minioClient.MakeBucket(userID.String(), "us-east-1")
		if err != nil {
			log.Println(err)
			return err
		}
	}

	tmpFileName := srv.tmpDir + "/" + userID.String() + "-" + mediaID
	log.Println(tmpFileName)
	incomingFile, err := os.Create(tmpFileName)
	if err != nil {
		log.Println(err)
		return err
	}
	defer incomingFile.Close()

	fileSize := 0
	for {
		data, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}

			log.Println(err)
			return err
		}
		fileName = data.FileName
		contentType = data.ContentType

		size, err := incomingFile.Write(data.Contents)
		if err != nil {
			log.Println(err)
			return err
		}
		fileSize += size
	}

	err = incomingFile.Sync()
	if err != nil {
		log.Println(err)
		return err
	}

	incomingFile.Close()

	_, err = srv.minioClient.FPutObject(userID.String(), mediaID, tmpFileName, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		log.Println(err)
	}

	os.Remove(tmpFileName)

	err = uploadMedia(srv, userID, mediaID, isEncrypted, fileName, contentType, fileSize)
	if err != nil {
		log.Println(err)
		stream.SendAndClose(nil)
		return err
	}

	err = stream.SendAndClose(&UploadMediaResponse{MediaID: mediaID})
	if err != nil {
		log.Println(err)
	}
	return err
}

// Prepares the uploaded media for group conversation
// When an image is uploaded for the first time it is uploaded to currently loggged in user's bucket
// Here it is moved to the group's bucket and resized
func (srv *Server) PrepareMediaForGroup(userID uuid.UUID, groupID, mediaID string) error {
	tmpFileName := srv.tmpDir + "/group." + userID.String() + "-" + mediaID

	src := minio.NewSourceInfo(userID.String(), mediaID, nil)
	dst, err := minio.NewDestinationInfo(groupID, mediaID, nil, nil)
	if err != nil {
		log.Println(err)
		return err
	}

	exists, err := srv.minioClient.BucketExists(groupID)
	if err != nil {
		log.Println(err)
	}

	if exists == false {
		err = srv.minioClient.MakeBucket(groupID, "us-east-1")
		if err != nil {
			log.Println(err)
			return err
		}
	}

	err = srv.minioClient.CopyObject(dst, src)
	if err != nil {
		log.Println(err)
		return err
	}

	err = srv.minioClient.FGetObject(userID.String(), mediaID, tmpFileName, minio.GetObjectOptions{})
	if err != nil {
		log.Println(err)
		return err
	}

	err = srv.minioClient.RemoveObject(userID.String(), mediaID)
	if err != nil {
		log.Println(err)
		return err
	}

	imgSrc, err := imaging.Open(tmpFileName)
	if err != nil {
		log.Println("failed to open image", err)
		return err
	}

	imgSrc = imaging.Resize(imgSrc, 140, 140, imaging.Lanczos)
	var b bytes.Buffer
	imaging.Encode(&b, imgSrc, imaging.PNG)

	defer os.Remove(tmpFileName)

	return updateGroupAvatar(srv, userID, groupID, mediaID, b.Bytes())
}

func (srv *Server) GetProfilePicture(in *GetProfilePictureRequest, stream Ngobrel_GetProfilePictureServer) error {
	userID, err := getUserID(srv, stream.Context())
	if err != nil {
		log.Println(err)
		return err
	}

	return in.getProfilePictureStream(srv, userID, stream)
}

func (srv *Server) GetMedia(in *GetMediaRequest, stream Ngobrel_GetMediaServer) error {
	userID, err := getUserID(srv, stream.Context())
	if err != nil {
		log.Println(err)
		return err
	}

	return in.getMediaStream(srv, userID, stream)
}

func (srv *Server) RegisterFCM(ctx context.Context, in *RegisterFCMRequest) (*RegisterFCMResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return in.RegisterFCM(srv, userID)
}

func (srv *Server) AckMessageNotificationStream(ctx context.Context, in *AckMessageNotificationStreamRequest) (*AckMessageNotificationStreamResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return in.AckMessageNotificationStream(userID)
}

func (srv *Server) PutMessageState(ctx context.Context, in *PutMessageStateRequest) (*PutMessageStateResponse, error) {
	userID, err := getUserID(srv, ctx)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	senderDeviceID, err := getDeviceID(srv, ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixNano() / 1000.0 // in microsecs
	nowFloat := float64(now) / 1000000.0  // in secs

	return in.PutMessageState(srv, userID, senderDeviceID, nowFloat)
}
