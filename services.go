package ngobrel

import (
	"errors"
	"time"

	uuid "github.com/satori/go.uuid"

	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
)

type Server struct{}

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

func getDeviceID(ctx context.Context) (uuid.UUID, error) {
	return getID(ctx, "device-id")
}

func getUserID(ctx context.Context) (uuid.UUID, error) {
	return getID(ctx, "user-id")
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
	err = in.putMessageToUserID(senderID, senderDeviceID, recipientID)
	if err != nil {
		return nil, err
	}
	return &PutMessageResponse{MessageID: int64(in.MessageID)}, nil
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
	return in.CreateProfile()
}
