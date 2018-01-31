package sqs

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"

	"github.com/zerofox-oss/go-msg"
)

// Failing receiver returns an error upon consumption of a Message.
type FailingReceiver struct {
	t *testing.T
}

func (r *FailingReceiver) Receive(ctx context.Context, m *msg.Message) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return errors.New("failing recevier returned error")
	}

}

// SimpleReceiver simply returns nil upon consumption of a Message.
type SimpleReceiver struct {
	t *testing.T
}

func (r *SimpleReceiver) Receive(ctx context.Context, m *msg.Message) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// newSQSMessages generates a slice of n sqs.Message objects and
// returns a pointer to that slice.
func newSQSMessages(n int) *[]*sqs.Message {
	messages := make([]*sqs.Message, n)

	for i := 0; i < n; i++ {
		messages[i] = &sqs.Message{
			Body:              aws.String(fmt.Sprintf("this is a test %d", i)),
			MessageAttributes: make(map[string]*sqs.MessageAttributeValue),
			MessageId:         aws.String(fmt.Sprintf("msg%d", i)),
			ReceiptHandle:     aws.String(fmt.Sprintf("msg%d", i)),
		}
	}

	return &messages
}

// TestServer_Serve tests that an SQS server can receive messages, process
// them, and delete them from the queue successfully.
func TestServer_Serve(t *testing.T) {
	msgs := newSQSMessages(1)
	mockSQS := newMockSQSAPI(msgs, t)
	srv := newMockServer(1, mockSQS)

	go func() {
		r := &SimpleReceiver{t: t}
		srv.Serve(r)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := mockSQS.WaitForAllDeletes(ctx); err != nil {
		t.Errorf(err.Error())
	}
}

// TestServer_ServeConcurrency tests that an SQS server can process a lot of
// messages using many concurrent goroutines.
func TestServer_Concurrency(t *testing.T) {
	msgs := newSQSMessages(10000)
	mockSQS := newMockSQSAPI(msgs, t)
	srv := newMockServer(100, mockSQS)

	go func() {
		r := &SimpleReceiver{t: t}
		srv.Serve(r)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := mockSQS.WaitForAllDeletes(ctx); err != nil {
		t.Fatalf(err.Error())
	}
}

// TestServer_Serve tests that an SQS server can receive messages, process
// them, and delete them from the queue successfully.
func TestServer_ServeFailingReceiver(t *testing.T) {
	msgs := newSQSMessages(1)
	mockSQS := newMockSQSAPI(msgs, t)
	srv := newMockServer(1, mockSQS)

	go func() {
		r := &FailingReceiver{t: t}
		srv.Serve(r)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := mockSQS.WaitForVisibilityTimeouts(ctx); err != nil {
		t.Errorf(err.Error())
	}
}

// Test conversion of sqs.Message.Attributes to msg.Attributes.
func TestServer_ConvertToMsgAttrs(t *testing.T) {
	val1 := "val1"
	val2 := "val2"
	str := "Attribute Test"
	awsMsg := &sqs.Message{
		Body: &str,
		MessageAttributes: map[string]*sqs.MessageAttributeValue{
			"key1": {StringValue: &val1},
			"key2": {StringValue: &val2},
		},
	}
	serv := &Server{}
	attrs := serv.convertToMsgAttrs(awsMsg.MessageAttributes)
	if attrs.Get("key1") != val1 {
		t.Error("key1 does not match")
	}
	if attrs.Get("key2") != val2 {
		t.Error("key2 does not match")
	}
}

// Test Shutdown with a nil context.Context.
func TestServer_ShutdownWithoutContext(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("We expected a panic, recover() was not nil")
		}
	}()

	srv := &Server{}
	srv.Shutdown(nil)
}

// Tests that ErrServerClosed when all go routines finish before the context
// cancels.
func TestServer_ShutdownClean(t *testing.T) {
	msgs := newSQSMessages(10)
	srv := newMockServer(1, newMockSQSAPI(msgs, t))
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	go func() {
		r := &SimpleReceiver{t: t}
		srv.Serve(r)
	}()

	err := srv.Shutdown(ctx)
	if err != msg.ErrServerClosed {
		t.Errorf("Expected ErrServerClosed, got %v", err)
	}
}

// Tests that srv.Shutdown() shuts down when the passed context is
// canceled (in this case, by timeout).
func TestServer_ShutdownHard(t *testing.T) {
	msgs := newSQSMessages(100)
	srv := newMockServer(1, newMockSQSAPI(msgs, t))
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	go func() {
		r := &SimpleReceiver{t: t}
		srv.Serve(r)
	}()

	err := srv.Shutdown(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}
}
