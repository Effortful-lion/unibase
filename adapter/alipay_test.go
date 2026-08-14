package adapter

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAlipayError(t *testing.T) {
	err := &AlipayError{Code: "40004", Msg: "Business Failed", SubMsg: "余额不足"}
	assert.Equal(t, "alipay: 40004: Business Failed: 余额不足", err.Error())

	assert.True(t, IsAlipayError(err))
	assert.True(t, IsAlipayError(&AlipayError{Code: "40004"}))
	assert.False(t, IsAlipayError(assert.AnError))
	assert.False(t, IsAlipayError(nil))
}

func TestNewAlipay_MissingConfig(t *testing.T) {
	_, err := NewAlipay(AlipayConfig{
		AppID: "202100014960xxxx",
	})
	assert.Error(t, err)
}

func TestAlipay_TradePagePay_NotInitialized(t *testing.T) {
	a := &Alipay{}
	_, err := a.TradePagePay(nil, TradePagePayReq{})
	assert.ErrorIs(t, err, ErrAlipayNotInit)
}

func TestAlipay_QueryOrder_NotInitialized(t *testing.T) {
	a := &Alipay{}
	_, err := a.QueryOrder(nil, "test-order")
	assert.ErrorIs(t, err, ErrAlipayNotInit)
}

func TestAlipay_VerifyCallback_NotInitialized(t *testing.T) {
	a := &Alipay{}
	_, err := a.VerifyCallback(url.Values{})
	assert.ErrorIs(t, err, ErrAlipayNotInit)
}

func TestAlipay_DecodeNotify_NotInitialized(t *testing.T) {
	a := &Alipay{}
	_, err := a.DecodeNotify(nil, nil)
	assert.ErrorIs(t, err, ErrAlipayNotInit)
}

func TestAlipay_ACKNotification_NotInitialized(t *testing.T) {
	a := &Alipay{}
	w := &mockResponseWriter{}
	a.ACKNotification(w)
	assert.Equal(t, http.StatusInternalServerError, w.code)
}

type mockResponseWriter struct {
	code int
}

func (m *mockResponseWriter) Header() http.Header         { return http.Header{} }
func (m *mockResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (m *mockResponseWriter) WriteHeader(code int)        { m.code = code }
