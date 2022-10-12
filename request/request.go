package request

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptrace"
	"net/url"
	"os"
	"strings"
	"time"
)

type Request struct {
	httpreq *http.Request
	Header  *http.Header
	Client  *traceClient
	Cookies []*http.Cookie
}

func (r *Request) withHttpTrace() {
	var client = traceClient{
		Client: &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},
		traceTime: &traceTime{},
		traceStat: &traceStat{},
	}
	r.Client = &client
	trace := &httptrace.ClientTrace{
		DNSStart: func(dsi httptrace.DNSStartInfo) {
			r.Client.traceTime.dnsStart = time.Now()
		},
		DNSDone: func(ddi httptrace.DNSDoneInfo) {
			r.Client.traceTime.dnsDone = time.Now()
		},
		ConnectStart: func(network, addr string) {
			if r.Client.traceTime.dnsDone.IsZero() {
				r.Client.traceTime.dnsDone = time.Now()
			}
		},
		ConnectDone: func(network, addr string, err error) {
			if err != nil {
				log.Fatalf("unable to connect to host %v: %v", addr, err)
			}
			r.Client.traceTime.connDone = time.Now()
		},

		GotConn: func(info httptrace.GotConnInfo) {
			r.Client.traceTime.gotConn = time.Now()
		},
		GotFirstResponseByte: func() {
			r.Client.traceTime.transferStart = time.Now()
		},
	}
	r.httpreq = r.httpreq.WithContext(httptrace.WithClientTrace(r.httpreq.Context(), trace))
}

type Header map[string]string
type Params map[string]string
type Datas map[string]string // for post form
type Files map[string]string // name ,filename

// Auth LIKE {username,password}
type Auth []string

func TraceRequests() *Request {
	req := new(Request)

	req.httpreq = &http.Request{
		Method:     "GET",
		Header:     make(http.Header),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}
	req.Header = &req.httpreq.Header
	req.withHttpTrace()
	// auto with Cookies
	// cookiejar.New source code return jar, nil
	jar, _ := cookiejar.New(nil)

	req.Client.Jar = jar

	return req
}

type traceClient struct {
	*http.Client
	traceTime *traceTime
	traceStat *traceStat
}

func (t *traceClient) Do(r *http.Request) (*http.Response, error) {
	res, err := t.Client.Do(r)

	if err != nil {
		return nil, err
	}
	done := time.Now()
	if t.traceTime.transferStart.IsZero() {
		t.traceTime.transferStart = done
	}
	if t.traceTime.dnsStart.IsZero() {
		t.traceTime.dnsStart = t.traceTime.dnsDone
	}
	t.traceStat = &traceStat{
		Status:           res.StatusCode,
		DNSLookup:        t.traceTime.dnsDone.Sub(t.traceTime.dnsStart).String(),
		TCPConnection:    t.traceTime.connDone.Sub(t.traceTime.dnsDone).String(),
		TLSHandshake:     t.traceTime.gotConn.Sub(t.traceTime.connDone).String(),
		ServerProcessing: t.traceTime.transferStart.Sub(t.traceTime.gotConn).String(),
		ContentTransfer:  done.Sub(t.traceTime.transferStart).String(),
		Total:            done.Sub(t.traceTime.dnsStart).String(),
	}
	return res, nil
}
func (t *traceClient) GetTraceStat() *traceStat {
	return t.traceStat
}

type traceTime struct {
	dnsStart      time.Time
	dnsDone       time.Time
	gotConn       time.Time
	connDone      time.Time
	transferStart time.Time
}

type traceStat struct {
	Status           int    `json:"http状态码"`
	DNSLookup        string `json:"DNS查找时间"`
	TCPConnection    string `json:"TCP连接时间"`
	TLSHandshake     string `json:"TLS握手时间"`
	ServerProcessing string `json:"服务器处理时间"`
	ContentTransfer  string `json:"数据传输时间"`
	Total            string `json:"总耗时"`
}

func (r *Request) Get(origurl string, args ...interface{}) (resp *Response, err error) {

	r.httpreq.Method = "GET"

	// set params ?a=b&b=c
	//set Header
	var params []map[string]string

	//reset Cookies,
	//Client.Do can copy cookie from client.Jar to req.Header
	delete(r.httpreq.Header, "Cookie")

	for _, arg := range args {
		switch a := arg.(type) {
		// arg is Header , set to request header
		case Header:

			for k, v := range a {
				r.Header.Set(k, v)
			}
			// arg is "GET" params
			// ?title=website&id=1860&from=login
		case Params:
			params = append(params, a)
		case Auth:
			// a{username,password}
			r.httpreq.SetBasicAuth(a[0], a[1])
		}
	}

	disturl, _ := buildURLParams(origurl, params...)

	//prepare to Do
	URL, err := url.Parse(disturl)
	if err != nil {
		return nil, err
	}
	r.httpreq.URL = URL

	r.ClientSetCookies()

	res, err := r.Client.Do(r.httpreq)

	if err != nil {
		return nil, err
	}

	resp = &Response{}
	resp.R = res
	resp.req = r

	resp.Content()
	defer res.Body.Close()

	return resp, nil
}

// handle URL params
func buildURLParams(userURL string, params ...map[string]string) (string, error) {
	parsedURL, err := url.Parse(userURL)

	if err != nil {
		return "", err
	}

	parsedQuery, err := url.ParseQuery(parsedURL.RawQuery)

	if err != nil {
		return "", nil
	}

	for _, param := range params {
		for key, value := range param {
			parsedQuery.Add(key, value)
		}
	}
	return addQueryParams(parsedURL, parsedQuery), nil
}

func addQueryParams(parsedURL *url.URL, parsedQuery url.Values) string {
	if len(parsedQuery) > 0 {
		return strings.Join([]string{strings.Replace(parsedURL.String(), "?"+parsedURL.RawQuery, "", -1), parsedQuery.Encode()}, "?")
	}
	return strings.Replace(parsedURL.String(), "?"+parsedURL.RawQuery, "", -1)
}

// SetCookie
// cookies only save to Client.Jar
// req.Cookies is temporary
func (r *Request) SetCookie(cookie *http.Cookie) {
	r.Cookies = append(r.Cookies, cookie)
}

func (r *Request) ClearCookies() {
	r.Cookies = r.Cookies[0:0]
}

func (r *Request) ClientSetCookies() {
	if len(r.Cookies) > 0 {
		// 1. Cookies have content, Copy Cookies to Client.jar
		// 2. Clear  Cookies
		r.Client.Jar.SetCookies(r.httpreq.URL, r.Cookies)
		r.ClearCookies()
	}

}

// SetTimeout set timeout s = second
func (r *Request) SetTimeout(n time.Duration) {
	r.Client.Timeout = time.Duration(n * time.Second)
}

func (r *Request) Close() {
	r.httpreq.Close = true
}

func (r *Request) Proxy(proxyurl string) {

	urli := url.URL{}
	urlproxy, err := urli.Parse(proxyurl)
	if err != nil {
		fmt.Println("Set proxy failed")
		return
	}
	r.Client.Transport = &http.Transport{
		Proxy:           http.ProxyURL(urlproxy),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

}

type Response struct {
	R       *http.Response
	content []byte
	text    string
	req     *Request
}

func (resp *Response) GetRequest() *Request {
	return resp.req
}

func (resp *Response) Content() []byte {

	var err error

	if len(resp.content) > 0 {
		return resp.content
	}

	var Body = resp.R.Body
	if resp.R.Header.Get("Content-Encoding") == "gzip" && resp.req.Header.Get("Accept-Encoding") != "" {
		// fmt.Println("gzip")
		reader, err := gzip.NewReader(Body)
		if err != nil {
			return nil
		}
		Body = reader
	}

	resp.content, err = ioutil.ReadAll(Body)
	if err != nil {
		return nil
	}

	return resp.content
}

func (resp *Response) Text() string {
	if resp.content == nil {
		resp.Content()
	}
	resp.text = string(resp.content)
	return resp.text
}

func (resp *Response) SaveFile(filename string) error {
	if resp.content == nil {
		resp.Content()
	}
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(resp.content)
	f.Sync()

	return err
}

func (resp *Response) Json(v interface{}) error {
	if resp.content == nil {
		resp.Content()
	}
	return json.Unmarshal(resp.content, v)
}

func (resp *Response) Cookies() (cookies []*http.Cookie) {
	httpreq := resp.req.httpreq
	client := resp.req.Client

	cookies = client.Jar.Cookies(httpreq.URL)

	return cookies

}

// POST requests

func (r *Request) PostJson(origurl string, args ...interface{}) (resp *Response, err error) {

	r.httpreq.Method = "POST"

	r.Header.Set("Content-Type", "application/json")

	//reset Cookies,
	//Client.Do can copy cookie from client.Jar to req.Header
	delete(r.httpreq.Header, "Cookie")

	for _, arg := range args {
		switch a := arg.(type) {
		// arg is Header , set to request header
		case Header:

			for k, v := range a {
				r.Header.Set(k, v)
			}
		case string:
			r.setBodyRawBytes(ioutil.NopCloser(strings.NewReader(arg.(string))))
		case Auth:
			// a{username,password}
			r.httpreq.SetBasicAuth(a[0], a[1])
		default:
			b := new(bytes.Buffer)
			err = json.NewEncoder(b).Encode(a)
			if err != nil {
				return nil, err
			}
			r.setBodyRawBytes(ioutil.NopCloser(b))
		}
	}

	//prepare to Do
	URL, err := url.Parse(origurl)
	if err != nil {
		return nil, err
	}
	r.httpreq.URL = URL

	r.ClientSetCookies()

	res, err := r.Client.Do(r.httpreq)

	// clear post  request information
	r.httpreq.Body = nil
	r.httpreq.GetBody = nil
	r.httpreq.ContentLength = 0

	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	resp = &Response{}
	resp.R = res
	resp.req = r

	resp.Content()
	defer res.Body.Close()
	return resp, nil
}

func (r *Request) Post(origurl string, args ...interface{}) (resp *Response, err error) {

	r.httpreq.Method = "POST"

	//set default
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// set params ?a=b&b=c
	//set Header
	params := []map[string]string{}
	datas := []map[string]string{} // POST
	files := []map[string]string{} //post file

	//reset Cookies,
	//Client.Do can copy cookie from client.Jar to req.Header
	delete(r.httpreq.Header, "Cookie")

	for _, arg := range args {
		switch a := arg.(type) {
		// arg is Header , set to request header
		case Header:

			for k, v := range a {
				r.Header.Set(k, v)
			}
			// arg is "GET" params
			// ?title=website&id=1860&from=login
		case Params:
			params = append(params, a)

		case Datas: //Post form data,packaged in body.
			datas = append(datas, a)
		case Files:
			files = append(files, a)
		case Auth:
			// a{username,password}
			r.httpreq.SetBasicAuth(a[0], a[1])
		}
	}

	disturl, _ := buildURLParams(origurl, params...)

	if len(files) > 0 {
		r.buildFilesAndForms(files, datas)

	} else {
		Forms := r.buildForms(datas...)
		r.setBodyBytes(Forms) // set forms to body
	}
	//prepare to Do
	URL, err := url.Parse(disturl)
	if err != nil {
		return nil, err
	}
	r.httpreq.URL = URL

	r.ClientSetCookies()

	res, err := r.Client.Do(r.httpreq)

	// clear post param
	r.httpreq.Body = nil
	r.httpreq.GetBody = nil
	r.httpreq.ContentLength = 0

	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	resp = &Response{}
	resp.R = res
	resp.req = r

	resp.Content()
	defer res.Body.Close()

	return resp, nil
}

// only set forms
func (r *Request) setBodyBytes(Forms url.Values) {

	// maybe
	data := Forms.Encode()
	r.httpreq.Body = ioutil.NopCloser(strings.NewReader(data))
	r.httpreq.ContentLength = int64(len(data))
}

// only set forms
func (r *Request) setBodyRawBytes(read io.ReadCloser) {
	r.httpreq.Body = read
}

// upload file and form
// build to body format
func (r *Request) buildFilesAndForms(files []map[string]string, datas []map[string]string) {

	//handle file multipart

	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	for _, file := range files {
		for k, v := range file {
			part, err := w.CreateFormFile(k, v)
			if err != nil {
				fmt.Printf("Upload %s failed!", v)
				panic(err)
			}
			file := openFile(v)
			_, err = io.Copy(part, file)
			if err != nil {
				panic(err)
			}
		}
	}

	for _, data := range datas {
		for k, v := range data {
			w.WriteField(k, v)
		}
	}

	w.Close()
	// set file header example:
	// "Content-Type": "multipart/form-data; boundary=------------------------7d87eceb5520850c",
	r.httpreq.Body = ioutil.NopCloser(bytes.NewReader(b.Bytes()))
	r.httpreq.ContentLength = int64(b.Len())
	r.Header.Set("Content-Type", w.FormDataContentType())
}

// build post Form data
func (r *Request) buildForms(datas ...map[string]string) (Forms url.Values) {
	Forms = url.Values{}
	for _, data := range datas {
		for key, value := range data {
			Forms.Add(key, value)
		}
	}
	return Forms
}

// open file for post upload files

func openFile(filename string) *os.File {
	r, err := os.Open(filename)
	if err != nil {
		panic(err)
	}
	return r
}
