package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStubService_CreateCheckout(t *testing.T) {
	s := NewStubService()
	tid := uuid.Must(uuid.NewV7())
	res, err := s.CreateCheckout(context.Background(), CheckoutRequest{
		TenantID:      tid,
		PlanCode:      "pro_99",
		CustomerEmail: "x@y.z",
		SuccessURL:    "https://app.test/success",
		CancelURL:     "https://app.test/cancel",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, res.CheckoutURL)
	assert.Contains(t, res.CheckoutURL, "/dev/checkout?session=")
	assert.Contains(t, res.CheckoutURL, "plan=pro_99")
	assert.NotEmpty(t, res.SessionID)
}

func TestStubService_PortalURL(t *testing.T) {
	s := NewStubService()
	tid := uuid.Must(uuid.NewV7())
	url, err := s.PortalURL(context.Background(), PortalRequest{
		TenantID:  tid,
		ReturnURL: "https://app.test/billing",
	})
	require.NoError(t, err)
	assert.Contains(t, url, "/dev/portal?tenant="+tid.String())
}

func TestStubService_CurrentSubscription(t *testing.T) {
	s := NewStubService()
	tid := uuid.Must(uuid.NewV7())
	sub, err := s.CurrentSubscription(context.Background(), tid)
	require.NoError(t, err)
	assert.Equal(t, "active", sub.Status)
	assert.Equal(t, "stub", sub.Provider)
}

func TestStubService_CancelSubscription(t *testing.T) {
	s := NewStubService()
	tid := uuid.Must(uuid.NewV7())
	sub, err := s.CancelSubscription(context.Background(), CancelRequest{TenantID: tid, AtPeriodEnd: true})
	require.NoError(t, err)
	assert.True(t, sub.CancelAtPeriodEnd)
	assert.Equal(t, "active", sub.Status)
}

func TestStubService_VerifyWebhookSignature(t *testing.T) {
	s := NewStubService()
	// Stub accepts everything — a real Stripe impl would refuse this payload.
	body := []byte(`{"id":"evt_test_1","type":"checkout.session.completed","data":{"object":{"subscription":"sub_123","customer":"cus_456","invoice":"in_789","amount_paid":9900,"currency":"thb","status":"paid"}}}`)
	ev, err := s.VerifyWebhookSignature(body, "t=1,v1=any")
	require.NoError(t, err)
	assert.Equal(t, "checkout.session.completed", ev.Type)
	assert.Equal(t, "evt_test_1", ev.ID)
	assert.Equal(t, "sub_123", ev.SubscriptionID)
	assert.Equal(t, "cus_456", ev.CustomerID)
	assert.Equal(t, "in_789", ev.InvoiceID)
	assert.Equal(t, int64(9900), ev.Amount)
	assert.Equal(t, "thb", ev.Currency)
	assert.Equal(t, "paid", ev.Status)
}

func TestStubService_VerifyWebhookSignature_InvalidJSON(t *testing.T) {
	s := NewStubService()
	_, err := s.VerifyWebhookSignature([]byte("not json"), "any")
	require.Error(t, err)
	var invalid ErrInvalidWebhook
	assert.True(t, errors.As(err, &invalid))
}

func TestNewStripeService_MissingKey(t *testing.T) {
	_, err := NewStripeService(Config{})
	assert.Error(t, err)
}