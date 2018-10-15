package ngobrel

import (
	"errors"
	"log"
	"time"

	uuid "github.com/satori/go.uuid"

	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
)

type Server struct {
	receiptStream map[string]Ngobrel_GetMessageNotificationServer
	smsClient     Sms
}

func NewServer(sms Sms) *Server {
	return &Server{
		receiptStream: make(map[string]Ngobrel_GetMessageNotificationServer),
		smsClient:     sms,
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
