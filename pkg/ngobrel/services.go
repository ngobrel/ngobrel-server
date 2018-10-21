package ngobrel

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"os"
	"time"

	"github.com/disintegration/imaging"

	"github.com/cespare/xxhash"

	uuid "github.com/satori/go.uuid"

	minio "github.com/minio/minio-go"

	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
)

type Server struct {
	receiptStream map[string]Ngobrel_GetMessageNotificationServer
	smsClient     Sms
	minioClient   minio.Client
	tmpDir        string
}

func NewServer(sms Sms, minioClient minio.Client) *Server {
	tmpDir := os.Getenv("TMPDIR")
	if tmpDir == "" {
		log.Println("TMPDIR environment variable is not set, using /tmp as temp directory.")
		tmpDir = "/tmp"
	}
	return &Server{
		receiptStream: make(map[string]Ngobrel_GetMessageNotificationServer),
		smsClient:     sms,
		minioClient:   minioClient,
		tmpDir:        tmpDir,
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

func getDeviceID(ctx context.Context) (uuid.UUID, error) {
	token, err := getToken(ctx)
	if err != nil {
		return uuid.Nil, err
	}

	id, err := getDeviceIDFromToken(token)
	return uuid.FromString(id)
}

func getUserID(ctx context.Context) (uuid.UUID, error) {
	token, err := getToken(ctx)
	if err != nil {
		return uuid.Nil, err
	}

	id, err := getUserIDFromToken(token)
	return uuid.FromString(id)
}

func (srv *Server) GetMessageNotification(in *GetMessagesRequest, stream Ngobrel_GetMessageNotificationServer) error {
	recipientDeviceID, err := getDeviceID(stream.Context())
	if err != nil {
		return err
	}

	return in.getMessageNotificationStream(srv, recipientDeviceID, stream)
}

func (srv *Server) GetMessages(in *GetMessagesRequest, stream Ngobrel_GetMessagesServer) error {
	recipientDeviceID, err := getDeviceID(stream.Context())
	if err != nil {
		return err
	}

	return in.getMessages(recipientDeviceID, stream)
}

func (srv *Server) PutMessage(ctx context.Context, in *PutMessageRequest) (*PutMessageResponse, error) {

	senderID, err := getUserID(ctx)
	if err != nil {
		return nil, err
	}
	senderDeviceID, err := getDeviceID(ctx)
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
	userID, err := getUserID(ctx)
	if err != nil {
		return nil, err
	}

	return in.CreateConversation(userID)
}

func (srv *Server) ListConversations(ctx context.Context, in *ListConversationsRequest) (*ListConversationsResponse, error) {
	userID, err := getUserID(ctx)
	if err != nil {
		return nil, err
	}

	return in.ListConversations(userID)
}

func (srv *Server) GetContacts(ctx context.Context, in *GetContactsRequest) (*GetContactsResponse, error) {
	userID, err := getUserID(ctx)
	if err != nil {
		return nil, err
	}

	return in.GetContacts(userID)
}

func (srv *Server) UpdateConversation(ctx context.Context, in *UpdateConversationRequest) (*UpdateConversationResponse, error) {
	userID, err := getUserID(ctx)
	if err != nil {
		return nil, err
	}

	return in.UpdateConversation(userID)
}

func (srv *Server) CreateProfile(ctx context.Context, in *CreateProfileRequest) (*CreateProfileResponse, error) {
	return in.CreateProfile(srv)
}

func (srv *Server) EditProfile(ctx context.Context, in *EditProfileRequest) (*EditProfileResponse, error) {
	userID, err := getUserID(ctx)
	if err != nil {
		return nil, err
	}
	return in.EditProfile(userID)
}

func (srv *Server) GetProfile(ctx context.Context, in *GetProfileRequest) (*GetProfileResponse, error) {
	userID, err := getUserID(ctx)
	if err != nil {
		return nil, err
	}
	return in.GetProfile(userID)
}

func (srv *Server) PutContact(ctx context.Context, in *PutContactRequest) (*PutContactResponse, error) {
	userID, err := getUserID(ctx)
	if err != nil {
		return nil, err
	}

	return in.PutContact(userID)
}

func (srv *Server) CreateGroupConversation(ctx context.Context, in *CreateGroupConversationRequest) (*CreateGroupConversationResponse, error) {
	userID, err := getUserID(ctx)
	if err != nil {
		return nil, err
	}

	return in.CreateGroupConversation(userID)
}

func (srv *Server) VerifyOTP(ctx context.Context, in *VerifyOTPRequest) (*VerifyOTPResponse, error) {

	return in.VerifyOTP()
}

func (srv *Server) ListGroupParticipants(ctx context.Context, in *ListGroupParticipantsRequest) (*ListGroupParticipantsResponse, error) {
	userID, err := getUserID(ctx)
	if err != nil {
		return nil, err
	}

	return in.ListGroupParticipants(userID)
}

func (srv *Server) RemoveAdminRole(ctx context.Context, in *RemoveAdminRoleRequest) (*RemoveAdminRoleResponse, error) {
	userID, err := getUserID(ctx)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	if userID.String() == in.UserID {
		err := errors.New("remove-admin-role-cant-remove-self")
		return nil, err
	}

	return in.RemoveAdminRole(userID)
}

func (srv *Server) RemoveFromGroup(ctx context.Context, in *RemoveFromGroupRequest) (*RemoveFromGroupResponse, error) {
	userID, err := getUserID(ctx)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	if userID.String() == in.UserID {
		err := errors.New("remove-from-group-cant-remove-self")
		return nil, err
	}

	return in.RemoveFromGroup(userID)
}

func (srv *Server) AddToGroup(ctx context.Context, in *AddToGroupRequest) (*AddToGroupResponse, error) {
	userID, err := getUserID(ctx)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return in.AddToGroup(userID)
}

func (srv *Server) ExitFromGroup(ctx context.Context, in *ExitFromGroupRequest) (*ExitFromGroupResponse, error) {
	userID, err := getUserID(ctx)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return in.ExitFromGroup(userID)
}

func (srv *Server) RenameGroup(ctx context.Context, in *RenameGroupRequest) (*RenameGroupResponse, error) {
	userID, err := getUserID(ctx)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return in.RenameGroup(userID)
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

	userID, err := getUserID(stream.Context())
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
		log.Println("failed to open image: %v", err)
		return err
	}
	src = imaging.Resize(src, 140, 140, imaging.Lanczos)
	var b bytes.Buffer
	//writer := bufio.NewWriter(&b)
	imaging.Encode(&b, src, imaging.PNG)

	os.Remove(tmpFileName)
	err = uploadProfilePicture(userID, mediaID, b.Bytes(), fileSize)
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

	userID, err := getUserID(stream.Context())
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

	err = uploadMedia(userID, mediaID, isEncrypted, fileName, contentType, fileSize)
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

func (srv *Server) GetProfilePicture(in *GetProfilePictureRequest, stream Ngobrel_GetProfilePictureServer) error {
	userID, err := getUserID(stream.Context())
	if err != nil {
		log.Println(err)
		return err
	}

	return in.getProfilePictureStream(srv, userID, stream)
}
