package mock_ngobrel_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"

	gomock "github.com/golang/mock/gomock"
	ngobrel "ngobrel.rocks/ngobrel"
	mock_ngobrel "ngobrel.rocks/ngobrel/mock_ngobrel"
)

/*
message PutMessageRequest {
    string  recipientID         = 1;
    int64   messageID           = 2;
    int64   messageTimestamp    = 3;
    string  messageContents     = 4;
    bool    messageEncrypted    = 5;
}
*/

type rpcMsg struct {
	msg proto.Message
}

func (r *rpcMsg) Matches(msg interface{}) bool {
	m, ok := msg.(proto.Message)
	if !ok {
		return false
	}
	return proto.Equal(m, r.msg)
}

func (r *rpcMsg) String() string {
	return fmt.Sprintf("is %s", r.msg)
}

func TestPutMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	fmt.Println("Omama")
	mockNgobrelClient := mock_ngobrel.NewMockNgobrelClient(ctrl)

	req := &ngobrel.PutMessageRequest{
		MessageID:   0,
		RecipientID: "a",
	}

	mockNgobrelClient.EXPECT().PutMessage(
		gomock.Any(),
		&rpcMsg{msg: req},
	).Return(nil, errors.New("o1"))

	testPutMessage(t, mockNgobrelClient)
}

func testPutMessage(t *testing.T, client ngobrel.NgobrelClient) {

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := client.PutMessage(ctx, &ngobrel.PutMessageRequest{MessageID: 0, RecipientID: "a"})
	if err != nil {
		t.Errorf("mocking failed")
	}
	t.Log("Reply : ", r.MessageID)

}
