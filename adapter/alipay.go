package adapter

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/Effortful-lion/unibase/logx"

	"github.com/smartwalle/alipay/v3"
)

// AlipayConfig 支付宝配置。
type AlipayConfig struct {
	// AppID 支付宝应用 ID，必填。
	AppID string

	// PrivateKey 应用私钥（支持 PKCS#1 / PKCS#8 格式 PEM），必填。
	PrivateKey string

	// AppCertPath 应用公钥证书文件路径，用于校验支付宝响应签名。
	AppCertPath string

	// AlipayRootCertPath 支付宝根证书文件路径。
	AlipayRootCertPath string

	// AlipayCertPath 支付宝公钥证书文件路径。
	AlipayCertPath string

	// ServerDomain 服务器域名，用于拼接回调地址，例如 "https://example.com"。
	ServerDomain string

	// NotifyPath 异步通知路径，拼接到 ServerDomain 后作为 NotifyURL，例如 "/alipay/notify"。
	NotifyPath string

	// IsSandbox 是否为沙箱环境，默认 false（生产环境）。
	IsSandbox bool
}

// Alipay 是支付宝支付的薄封装。
// 持有标准库的 *alipay.Client，核心能力直接委托给原始客户端。
type Alipay struct {
	client       *alipay.Client
	serverDomain string
	notifyPath   string
	logger       *logx.Logger
}

// NewAlipay 创建支付宝适配器。
// 加载证书失败时返回 error。
func NewAlipay(cfg AlipayConfig) (*Alipay, error) {
	client, err := alipay.New(cfg.AppID, cfg.PrivateKey, !cfg.IsSandbox)
	if err != nil {
		return nil, err
	}

	if err = client.LoadAppPublicCertFromFile(cfg.AppCertPath); err != nil {
		return nil, err
	}
	if err = client.LoadAliPayPublicCertFromFile(cfg.AlipayCertPath); err != nil {
		return nil, err
	}
	if err = client.LoadAliPayRootCertFromFile(cfg.AlipayRootCertPath); err != nil {
		return nil, err
	}

	return &Alipay{
		client:       client,
		serverDomain: cfg.ServerDomain,
		notifyPath:   cfg.NotifyPath,
		logger:       logx.Module("adapter.alipay"),
	}, nil
}

// Client 返回底层的 *alipay.Client，可直接使用 alipay SDK 的全部能力。
func (a *Alipay) Client() *alipay.Client { return a.client }

// ── 支付 ──────────────────────────────────────────────────────

// TradePagePayReq 电脑网站支付请求参数。
type TradePagePayReq struct {
	// OutTradeNo 商户订单号，由商户自定义，需保证唯一。
	OutTradeNo string

	// TotalAmount 订单总金额，单位：元，精确到小数点后两位。
	TotalAmount string

	// Subject 订单标题，必填。
	Subject string

	// Body 订单描述，可选。
	Body string

	// ProductCode 销售产品码，固定值 "FAST_INSTANT_TRADE_PAY"。
	ProductCode string
}

// PayURL 支付跳转地址。
type PayURL struct {
	// URL 收银台页面地址，调用方将用户浏览器重定向到此处。
	URL string
}

// TradePagePay 发起电脑网站支付，返回支付宝收银台页面地址。
func (a *Alipay) TradePagePay(_ context.Context, req TradePagePayReq) (*PayURL, error) {
	if a == nil || a.client == nil {
		return nil, ErrAlipayNotInit
	}
	p := alipay.TradePagePay{
		Trade: alipay.Trade{
			Subject:     req.Subject,
			OutTradeNo:  req.OutTradeNo,
			TotalAmount: req.TotalAmount,
			ProductCode: req.ProductCode,
			Body:        req.Body,
		},
	}
	p.NotifyURL = a.serverDomain + a.notifyPath
	p.ReturnURL = a.serverDomain + a.notifyPath + "/callback"

	url, err := a.client.TradePagePay(p)
	if err != nil {
		a.logger.Error("alipay trade page pay failed", logx.Fields{"error": err, "out_trade_no": req.OutTradeNo})
		return nil, err
	}

	return &PayURL{URL: url.String()}, nil
}

// ── 查询 ──────────────────────────────────────────────────────

// OrderStatus 订单状态。
type OrderStatus struct {
	// OutTradeNo 商户订单号。
	OutTradeNo string

	// TradeNo 支付宝交易号。
	TradeNo string

	// Status 交易状态：WAIT_BUYER_PAY / TRADE_CLOSED / TRADE_SUCCESS / TRADE_FINISHED。
	Status string

	// TotalAmount 订单总金额。
	TotalAmount string

	// PayTime 支付时间，未支付为空。
	PayTime time.Time

	// Raw 原始响应，便于扩展。
	Raw *alipay.TradeQueryRsp
}

// QueryOrder 查询订单状态。
func (a *Alipay) QueryOrder(_ context.Context, outTradeNo string) (*OrderStatus, error) {
	if a == nil || a.client == nil {
		return nil, ErrAlipayNotInit
	}
	p := alipay.TradeQuery{OutTradeNo: outTradeNo}
	rsp, err := a.client.TradeQuery(p)
	if err != nil {
		a.logger.Error("alipay query order failed", logx.Fields{"error": err, "out_trade_no": outTradeNo})
		return nil, err
	}

	if !rsp.IsSuccess() {
		return nil, &AlipayError{
			Code:   rsp.Content.Code,
			Msg:    rsp.Content.Msg,
			SubMsg: rsp.Content.SubMsg,
		}
	}

	payTime, _ := time.Parse("2006-01-02 15:04:05", rsp.Content.SendPayDate)

	return &OrderStatus{
		OutTradeNo:  rsp.Content.OutTradeNo,
		TradeNo:     rsp.Content.TradeNo,
		Status:      rsp.Content.TradeStatus,
		TotalAmount: rsp.Content.TotalAmount,
		PayTime:     payTime,
		Raw:         rsp,
	}, nil
}

// ── 回调验签 ──────────────────────────────────────────────────

// CallbackResult 同步回调验证结果。
type CallbackResult struct {
	// OutTradeNo 商户订单号。
	OutTradeNo string

	// TradeNo 支付宝交易号。
	TradeNo string

	// TradeStatus 交易状态。
	TradeStatus string

	// TotalAmount 订单金额。
	TotalAmount string

	// Raw 原始表单数据。
	Raw url.Values
}

// VerifyCallback 验证支付宝同步回调（页面跳转回调）。
// 传入支付宝跳转回来时携带的查询参数，验签通过后返回结构化结果。
func (a *Alipay) VerifyCallback(form url.Values) (*CallbackResult, error) {
	if a == nil || a.client == nil {
		return nil, ErrAlipayNotInit
	}
	if _, err := a.client.VerifySign(form); err != nil {
		a.logger.Error("alipay callback verify sign failed", logx.Fields{"error": err})
		return nil, err
	}

	return &CallbackResult{
		OutTradeNo:  form.Get("out_trade_no"),
		TradeNo:     form.Get("trade_no"),
		TradeStatus: form.Get("trade_status"),
		TotalAmount: form.Get("total_amount"),
		Raw:         form,
	}, nil
}

// ── 异步通知 ──────────────────────────────────────────────────

// NotifyResult 异步通知解析结果。
type NotifyResult struct {
	// OutTradeNo 商户订单号。
	OutTradeNo string

	// TradeNo 支付宝交易号。
	TradeNo string

	// TradeStatus 交易状态。
	TradeStatus string

	// TotalAmount 订单金额。
	TotalAmount string

	// PayTime 支付时间。
	PayTime time.Time

	// Raw 原始通知数据。
	Raw *alipay.TradeNotification
}

// DecodeNotify 解析支付宝异步通知。
// req 为原始 HTTP 请求（需包含 POST form 数据）。
// 调用方需先调用 a.AckNotification(w) 确认收到通知，再处理业务逻辑。
func (a *Alipay) DecodeNotify(ctx context.Context, req *http.Request) (*NotifyResult, error) {
	if a == nil || a.client == nil {
		return nil, ErrAlipayNotInit
	}
	notification, err := a.client.GetTradeNotification(req)
	if err != nil {
		a.logger.Error("alipay decode notify failed", logx.Fields{"error": err})
		return nil, err
	}

	// 二次验签确认：通过 trade_no 查询订单详情
	p := alipay.TradeQuery{TradeNo: notification.TradeNo}
	rsp, err := a.client.TradeQuery(p)
	if err != nil {
		a.logger.Error("alipay notify query order failed", logx.Fields{"error": err, "trade_no": notification.TradeNo})
		return nil, err
	}
	if !rsp.IsSuccess() {
		return nil, &AlipayError{
			Code:   rsp.Content.Code,
			Msg:    rsp.Content.Msg,
			SubMsg: rsp.Content.SubMsg,
		}
	}

	payTime, _ := time.Parse("2006-01-02 15:04:05", rsp.Content.SendPayDate)

	return &NotifyResult{
		OutTradeNo:  notification.OutTradeNo,
		TradeNo:     notification.TradeNo,
		TradeStatus: notification.TradeStatus,
		TotalAmount: notification.TotalAmount,
		PayTime:     payTime,
		Raw:         notification,
	}, nil
}

// ACKNotification 向支付宝回复成功接收通知的响应。
// 调用方在 DecodeNotify 成功后、处理完业务逻辑前调用。
func (a *Alipay) ACKNotification(w http.ResponseWriter) {
	if a == nil || a.client == nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	a.client.AckNotification(w)
}
