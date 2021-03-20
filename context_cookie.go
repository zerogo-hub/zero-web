package zeroapi

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/zerogo-hub/zero-helper/crypto"
	"github.com/zerogo-hub/zero-helper/time"
)

// CookieOption cookie 选项
type CookieOption func(cookie *http.Cookie) error

// Cookie cookie 相关
type Cookie interface {

	// Cookie 获取 cookie 值
	Cookie(key string, opts ...CookieOption) (string, error)

	// SetCookie 设置 cookie，见 https://tools.ietf.org/html/rfc6265
	// key: cookie 参数名称
	// value: cookie 值
	// maxAge: 见 https://tools.ietf.org/html/rfc6265#section-4.1.2.2
	// 		 = 0: 表示不指定
	// 		 < 0: 表示立即删除
	// 		 > 0: cookie 生存时间，单位秒
	// domain: 见 https://tools.ietf.org/html/rfc6265#section-4.1.2.3
	// path: 见 https://tools.ietf.org/html/rfc6265#section-4.1.2.4
	// secure: 见 https://tools.ietf.org/html/rfc6265#section-4.1.2.5
	// httpOnly: 见 https://tools.ietf.org/html/rfc6265#section-4.1.2.6
	SetCookie(key, value string, opts ...CookieOption)

	// RemoveCookie 移除指定的 cookie
	RemoveCookie(key string, opts ...CookieOption)

	// SetHTTPCookie 设置原始的 cookie
	SetHTTPCookie(cookie *http.Cookie)

	// HTTPCookies 获取所有原始的 cookie
	HTTPCookies() []*http.Cookie
}

// Cookie 获取 cookie 值
func (ctx *context) Cookie(name string, opts ...CookieOption) (string, error) {
	oname := name

	if ctx.app.IsCookieEncode() {
		handler := ctx.app.CookieEncodeHandler()
		name = handler(name)
	}

	cookie, err := ctx.req.Cookie(name)
	if err != nil {
		return "", err
	}

	cookie.Name = oname

	if ctx.app.IsCookieEncode() {
		handler := ctx.app.CookieDecodeHandler()
		value, err := handler(cookie.Value)
		if err != nil {
			return "", err
		}

		cookie.Value = value
	}

	for _, opt := range opts {
		if err := opt(cookie); err != nil {
			return "", err
		}
	}

	val, err := url.QueryUnescape(cookie.Value)
	return val, err
}

// SetCookie 设置 cookie，见 https://tools.ietf.org/html/rfc6265
// key: cookie 参数名称
// value: cookie 值
// maxAge: 见 https://tools.ietf.org/html/rfc6265#section-4.1.2.2
// 		 = 0: 表示不指定
// 		 < 0: 表示立即删除
// 		 > 0: cookie 生存时间，单位秒
// domain: 见 https://tools.ietf.org/html/rfc6265#section-4.1.2.3
// path: 见 https://tools.ietf.org/html/rfc6265#section-4.1.2.4
// secure: 见 https://tools.ietf.org/html/rfc6265#section-4.1.2.5
// httpOnly: 见 https://tools.ietf.org/html/rfc6265#section-4.1.2.6
func (ctx *context) SetCookie(name, value string, opts ...CookieOption) {
	cookie := &http.Cookie{Name: name, Value: url.QueryEscape(value)}

	for _, opt := range opts {
		opt(cookie)
	}

	// 默认存在 1 小时
	if cookie.MaxAge == 0 {
		cookie.MaxAge = 3600
	}

	if ctx.app.IsCookieEncode() {
		handler := ctx.app.CookieEncodeHandler()
		cookie.Name = handler(cookie.Name)
		cookie.Value = handler(cookie.Value)
	}

	http.SetCookie(ctx.res.Writer(), cookie)
}

// RemoveCookie 移除指定的 cookie
func (ctx *context) RemoveCookie(name string, opts ...CookieOption) {
	ctx.SetCookie(name, "", WithCookieMaxAge(-1))
}

// SetHTTPCookie 设置原始的 cookie
func (ctx *context) SetHTTPCookie(cookie *http.Cookie) {
	if cookie == nil {
		panic("Cookie cannot be empty")
	}

	http.SetCookie(ctx.res.Writer(), cookie)
}

// HTTPCookies 获取所有原始的 cookie
func (ctx *context) HTTPCookies() []*http.Cookie {
	return ctx.req.Cookies()
}

// WithCookieMaxAge ..
// maxAge: 见 https://tools.ietf.org/html/rfc6265#section-4.1.2.2
// 		 = 0: 表示不指定存活时间
// 		 < 0: 表示立即删除
// 		 > 0: cookie 生存时间，单位秒
func WithCookieMaxAge(maxAge int) CookieOption {
	return func(cookie *http.Cookie) error {
		cookie.MaxAge = maxAge
		return nil
	}
}

// WithCookiePath path: https://tools.ietf.org/html/rfc6265#section-4.1.2.4
func WithCookiePath(path string) CookieOption {
	return func(cookie *http.Cookie) error {
		if path != "" {
			cookie.Path = path
		}
		return nil
	}
}

// WithCookieDomain domain: https://tools.ietf.org/html/rfc6265#section-4.1.2.3
func WithCookieDomain(domain string) CookieOption {
	return func(cookie *http.Cookie) error {
		if domain != "" {
			cookie.Domain = domain
		}
		return nil
	}
}

// WithCookieSecure secure: https://tools.ietf.org/html/rfc6265#section-4.1.2.5
func WithCookieSecure(secure bool) CookieOption {
	return func(cookie *http.Cookie) error {
		cookie.Secure = secure
		return nil
	}
}

// WithCookieHTTPOnly secure: https://tools.ietf.org/html/rfc6265#section-4.1.2.6
func WithCookieHTTPOnly(httpOnly bool) CookieOption {
	return func(cookie *http.Cookie) error {
		cookie.HttpOnly = httpOnly
		return nil
	}
}

// WithCookieSign 对 cookie 进行签名
func WithCookieSign(signKey string) CookieOption {
	return func(cookie *http.Cookie) error {
		if cookie.Name == "" {
			return errors.New("cookie name is empty")
		}

		timestamp := strconv.Itoa(int(time.Now()))

		buf := cookieBuffer()
		defer cookeReleaseBuffer(buf)

		buf.WriteString(cookie.Name)
		buf.WriteString(cookie.Value)
		buf.WriteString(timestamp)

		sign := crypto.HmacMd5(buf.String(), signKey)

		buf.Reset()
		buf.WriteString(cookie.Value)
		buf.WriteString("|")
		buf.WriteString(timestamp)
		buf.WriteString("|")
		buf.WriteString(sign)

		cookie.Value = buf.String()

		return nil
	}
}

// WithCookieVerify 对有签名的 cookie 进行验证
func WithCookieVerify(signKey string) CookieOption {
	return func(cookie *http.Cookie) error {
		if cookie.Value == "" {
			return errors.New("cookie value is empty")
		}

		l := strings.Split(cookie.Value, "|")
		if len(l) != 3 {
			// cookie 值被篡改
			cookie.Value = ""
			return errors.New("invalid cookie value 1")
		}

		value := l[0]
		timestamp := l[1]
		sign := l[2]

		buf := cookieBuffer()
		defer cookeReleaseBuffer(buf)

		buf.WriteString(cookie.Name)
		buf.WriteString(value)
		buf.WriteString(timestamp)
		calcSign := crypto.HmacMd5(buf.String(), signKey)

		if calcSign != sign {
			// cookie 值被篡改
			cookie.Value = ""
			return errors.New("invalid cookie value 2")
		}

		cookie.Value = value
		return nil
	}
}

var cookieBufferPool *sync.Pool

// cookieBuffer 从池中获取 buffer
func cookieBuffer() *bytes.Buffer {
	buff := cookieBufferPool.Get().(*bytes.Buffer)
	buff.Reset()
	return buff
}

// cookeReleaseBuffer 将 buff 放入池中
func cookeReleaseBuffer(buff *bytes.Buffer) {
	cookieBufferPool.Put(buff)
}

func init() {
	cookieBufferPool = &sync.Pool{}
	cookieBufferPool.New = func() interface{} {
		return &bytes.Buffer{}
	}
}
