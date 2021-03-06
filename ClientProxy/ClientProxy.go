package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	//"dns-master"
	"errors"
	"flag"
	"fmt"
	"github.com/miekg/dns"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// flag whether we want to emit debug output
var DEBUG bool = false
var LOG bool = false

// called for debug output
func _D(fmt string, v ...interface{}) {
	if DEBUG {
		log.Printf(fmt, v...)
	}
}

func _LOG(fmt string, v ...interface{}) {
	if LOG {
		log.Printf(fmt, v...)
	}
}

func searchServerIP(domain string, version int, DNSservers []string) (answer *dns.Msg, err error) {
	DNSserver := DNSservers[rand.Intn(len(DNSservers))]
	for i := 1; i <= 3; i++ {
		if DNSserver == "" {
			DNSserver = DNSservers[rand.Intn(len(DNSservers))]
		}
	}
	if DNSserver == "" {
		return nil, errors.New("DNSserver is an empty string")
	}
	dnsRequest := new(dns.Msg)
	if dnsRequest == nil {
		return nil, errors.New("Can not new dnsRequest")
	}
	dnsClient := new(dns.Client)
	if dnsClient == nil {
		return nil, errors.New("Can not new dnsClient")
	}
	if version == 4 {
		dnsRequest.SetQuestion(domain+".", dns.TypeA)
	} else if version == 6 {
		dnsRequest.SetQuestion(domain+".", dns.TypeAAAA)
	} else {
		return nil, errors.New("wrong parameter in version")
	}
	dnsRequest.SetEdns0(4096, true)
	answer, _, err = dnsClient.Exchange(dnsRequest, DNSserver)
	if err != nil {
		return nil, err
	}
	return answer, nil
}
func (this ClientProxy) getServerIP() error {
	var dns_servers []string
	dnsClient := new(dns.Client)
	if dnsClient == nil {
		return errors.New("Can not new dns Client")
	}
	dnsClient.WriteTimeout = this.timeout
	dnsClient.ReadTimeout = this.timeout
	for _, serverstring := range this.SERVERS {
		ipaddress := net.ParseIP(serverstring)
		if ipaddress != nil {
			dns_servers = append(dns_servers, serverstring)
		} else {
			//used for unitest need to delete after test.
			/*if strings.EqualFold(serverstring, "example.com") {
				dns_servers = append(dns_servers, "127.0.0.1")
				continue
			}
			IPResult, err := net.LookupIP(serverstring)
			if err == nil {
				for _, appendStr := range IPResult {
					dns_servers = append(dns_servers, appendStr.String())
				}
			} else {

				return err
			}*/
			dnsResponse, err := searchServerIP(serverstring, 4, this.DNS_SERVERS)
			if err != nil {
				for i := 0; i < len(dnsResponse.Answer); i++ {
					dns_servers = append(dns_servers, dnsResponse.Answer[i].String())
				}
			} else {
				return err
			}
			dnsResponse, err = searchServerIP(serverstring, 6, this.DNS_SERVERS)
			if err == nil {
				for i := 0; i < len(dnsResponse.Answer); i++ {
					dns_servers = append(dns_servers, "["+dnsResponse.Answer[i].String()+"]")
				}
			} else {

				return err
			}
		}
	}
	this.SERVERS = dns_servers
	return nil
}
func fockHTTPServer(req *http.Request, support_version bool) (error, *http.Response) {
	if support_version {
		contentType := req.Header.Get("Content-Type:")
		if contentType != "application/octet-stream" {
			return errors.New("Content-Type: unmatched"), nil
		}
		if strings.EqualFold(req.Method, "POST") {
			return errors.New("method unmatched"), nil
		}
		protocol := req.Header.Get("application/X-DNSoverHTTP")
		if strings.EqualFold(protocol, "UDP") || strings.EqualFold(protocol, "TCP") {
			return errors.New("protocol isn't UDP or TCP"), nil
		}
		return nil, new(http.Response)
	} else {
		if strings.EqualFold(req.Method, "POST") {
			return errors.New("method unmatched"), nil
		}
		contentType := req.Header.Get("Content-Type:")
		if contentType != "application/X-DNSoverHTTP" {
			return errors.New("Content-Type: unmatched"), nil
		}
		protocol := req.Header.Get("X-Proxy-DNS-Transport")
		if strings.EqualFold(protocol, "UDP") || strings.EqualFold(protocol, "TCP") {
			return errors.New("protocol isn't UDP or TCP"), nil
		}
		res := new(http.Response)
		res.Body = req.Body
		return nil, res
	}
}

/*
func (this ClientProxy) CreateHTTPClient() {
	if this.start_TLS == false {
		this.client = &http.Client{}
	} else {
		if this.TLS_Path == "" {
			tr := &http.Transport{
				TLSClientConfig:    &tls.Config{InsecureSkipVerify: true},
				DisableCompression: true}
			this.client = &http.Client{Transport: tr}
		} else {
			pool := x509.NewCertPool()
			caCrt, err := ioutil.ReadFile(this.TLS_Path)
			if err != nil {
				_D("invalid cetificate path: %s", this.TLS_Path)
				return
			}
			pool.AppendCertsFromPEM(caCrt)
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: pool},
			}
			this.client = &http.Client{Transport: tr}
		}
	}
}
*/
func (this ClientProxy) ServeDNS(w dns.ResponseWriter, request *dns.Msg) {
	_LOG("get %s query from %s", request.Question[0].Name, w.RemoteAddr())
	request_bytes, err := request.Pack() //I am not sure it is better to pack directly or using a pointer
	if err != nil {
		SRVFAIL(w, request)
		_D("error in packing request from %s for '%s', error message: %s",
			dns.ResponseWriter.RemoteAddr(w), request.Question[0].Name, err)
		return
	}
	ServerInput := this.SERVERS[rand.Intn(len(this.SERVERS))]
	ipaddress := net.ParseIP(ServerInput)
	var ServerInputurl string
	if this.start_TLS { //if it is TLS, use HTTPS
		if ipaddress.To4() != nil {
			ServerInputurl = "https://" + ServerInput
		} else {
			ServerInputurl = "https://[" + ServerInput + "]"
		}

	} else {
		if ipaddress.To4() != nil {
			ServerInputurl = "http://" + ServerInput
		} else {
			ServerInputurl = "http://[" + ServerInput + "]"
		}
	}

	postBytesReader := bytes.NewReader(request_bytes)

	ServerInputurl = ServerInputurl + "/proxy_dns"

	req, err := http.NewRequest("POST", ServerInputurl, postBytesReader) //need add random here in future
	if err != nil {
		SRVFAIL(w, request)
		_D("error in creating HTTP request from %s for '%s', error message: %s",
			dns.ResponseWriter.RemoteAddr(w), request.Question[0].Name, err)
		return
	}
	req.Header.Add("Host", ServerInput)
	req.Header.Add("Accept", "application/octet-stream")
	req.Header.Add("Content-Type", "application/octet-stream")
	if this.TransPro == UDPcode {
		req.Header.Add("Proxy-DNS-Transport", "UDP")
	} else if this.TransPro == TCPcode {
		req.Header.Add("Proxy-DNS-Transport", "TCP")
	}
	if this.start_TLS == false {
		//HTTP version
		tr := &http.Transport{
			DisableKeepAlives: true,
			TLSNextProto:      nil}
		this.client = &http.Client{Transport: tr}
	} else {
		//HTTPS version disabled certificate verification
		if this.TLS_Path == "" {
			tr := &http.Transport{
				TLSClientConfig:    &tls.Config{InsecureSkipVerify: true},
				DisableCompression: true}
			this.client = &http.Client{Transport: tr}
		} else {
			//HTTPS version allow the certificate manually
			pool := x509.NewCertPool()
			caCrt, err := ioutil.ReadFile(this.TLS_Path)
			if err != nil {
				_D("invalid cetificate path: %s", this.TLS_Path)
				return
			}
			pool.AppendCertsFromPEM(caCrt)
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: pool},
			}
			this.client = &http.Client{Transport: tr}
		}
	}
	resp, err := this.client.Do(req)
	if err != nil {
		SRVFAIL(w, request)
		_D("error in HTTP post request for query from %s for '%s', error message: %s",
			dns.ResponseWriter.RemoteAddr(w), request.Question[0].Name, err)
		return
	}
	if resp.StatusCode >= 500 {
		SRVFAIL(w, request)
		_D("HTTP ERROR: %s", http.StatusText(resp.StatusCode))
	}
	var requestBody []byte
	requestBody, err = ioutil.ReadAll(resp.Body)
	//	nRead, err := resp.Body.Read(requestBody)
	if err != nil {
		// these need to be separate checks, otherwise you will get a nil-reference
		// when you print the error message below!
		SRVFAIL(w, request)
		_D("error in reading HTTP response for query from %s for '%s', error message: %s",
			dns.ResponseWriter.RemoteAddr(w), request.Question[0].Name, err)
		return
	}
	//I not sure whether I should return server fail directly
	//I just found there is a bug here. Body.Read can not read all the contents out, I don't know how to solve it.
	if len(requestBody) < (int)(resp.ContentLength) {
		SRVFAIL(w, request)
		_D("failure reading all HTTP content for query from %s for '%s' (%d of %d bytes read)",
			dns.ResponseWriter.RemoteAddr(w), request.Question[0].Name,
			len(requestBody), (int)(resp.ContentLength))
		return
	}
	var DNSreponse dns.Msg
	err = DNSreponse.Unpack(requestBody)
	if err != nil {
		SRVFAIL(w, request)
		_D("error in packing HTTP response for query from %s for '%s', error message: %s",
			dns.ResponseWriter.RemoteAddr(w), request.Question[0].Name, err)
		return
	}
	err = w.WriteMsg(&DNSreponse)
	if err != nil {
		_D("error in sending DNS response back for query from %s for '%s', error message: %s",
			dns.ResponseWriter.RemoteAddr(w), request.Question[0].Name, err)
		return
	}
}

func SRVFAIL(w dns.ResponseWriter, req *dns.Msg) {
	m := new(dns.Msg)
	m.SetRcode(req, dns.RcodeServerFailure)
	w.WriteMsg(m)
}

type ClientProxy struct {
	ACCESS      []*net.IPNet
	SERVERS     []string
	s_len       int
	entries     int64
	max_entries int64
	NOW         int64
	giant       *sync.RWMutex
	timeout     time.Duration
	TransPro    int //specify for transmit protocol
	DNS_SERVERS []string
	start_TLS   bool
	TLS_Path    string
	client      *http.Client
}

const UDPcode = 1
const TCPcode = 2

func main() {
	fmt.Printf("Starting ClientProxy\n")
	var (
		S_SERVERS       string
		S_LISTEN        string
		S_ACCESS        string
		timeout         int
		max_entries     int64
		expire_interval int64
		S_DNS_SERVERS   string
		start_TLS       bool
		TLS_Path        string
	)
	flag.BoolVar(&LOG, "log", false, "whether print all query")
	flag.StringVar(&S_SERVERS, "proxy", "24.104.150.237", "we proxy requests to those servers,input like fci.biilab.cn") //Not sure use IP or URL, default server undefined
	flag.StringVar(&S_LISTEN, "listen", "[::]:53", "listen on (both tcp and udp)")
	flag.StringVar(&S_ACCESS, "access", "127.0.0.0/8,10.0.0.0/8", "allow those networks, use 0.0.0.0/0 to allow everything")
	flag.IntVar(&timeout, "timeout", 5, "timeout")
	flag.Int64Var(&expire_interval, "expire_interval", 300, "delete expired entries every N seconds")
	flag.BoolVar(&DEBUG, "debug", false, "enable/disable debug")
	flag.Int64Var(&max_entries, "max_cache_entries", 2000000, "max cache entries")
	flag.StringVar(&S_DNS_SERVERS, "dns_server", "114.114.114.114:53", "DNS server for initial server lookup")
	flag.BoolVar(&start_TLS, "start_TLS", false, "Whether use HTTPS to increase privacy.")
	flag.StringVar(&TLS_Path, "certificate_path", "", "The path of certificate, use no input to let client not validate certificate")
	flag.Parse()
	servers := strings.Split(S_SERVERS, ",")
	dns_servers := strings.Split(S_DNS_SERVERS, ",")
	UDPproxyer := ClientProxy{
		giant:       new(sync.RWMutex),
		ACCESS:      make([]*net.IPNet, 0),
		SERVERS:     servers,
		s_len:       len(servers),
		NOW:         time.Now().UTC().Unix(),
		entries:     0,
		timeout:     time.Duration(timeout) * time.Second,
		max_entries: max_entries,
		TransPro:    UDPcode,
		DNS_SERVERS: dns_servers,
		start_TLS:   start_TLS,
		TLS_Path:    TLS_Path}
	TCPproxyer := ClientProxy{
		giant:       new(sync.RWMutex),
		ACCESS:      make([]*net.IPNet, 0),
		SERVERS:     servers,
		s_len:       len(servers),
		NOW:         time.Now().UTC().Unix(),
		entries:     0,
		timeout:     time.Duration(timeout) * time.Second,
		max_entries: max_entries,
		TransPro:    TCPcode,
		DNS_SERVERS: dns_servers,
		start_TLS:   start_TLS,
		TLS_Path:    TLS_Path}
	for _, mask := range strings.Split(S_ACCESS, ",") {
		_, cidr, err := net.ParseCIDR(mask)
		if err != nil {
			panic(err)
		}
		_D("added access for %s\n", mask)
		UDPproxyer.ACCESS = append(UDPproxyer.ACCESS, cidr)
		TCPproxyer.ACCESS = append(TCPproxyer.ACCESS, cidr)
	}
	err := UDPproxyer.getServerIP()
	if err != nil {
		_D("can not get server address, %s\n", err)
		return
	}
	err = TCPproxyer.getServerIP()
	if err != nil {
		_D("can not get server address, %s\n", err)
		return
	}
	for _, addr := range strings.Split(S_LISTEN, ",") {
		fmt.Printf("listening @ %s\n", addr)
		go func() {
			if err := dns.ListenAndServe(addr, "udp", UDPproxyer); err != nil {
				log.Fatal(err)
			}
		}()

		go func() {
			if err := dns.ListenAndServe(addr, "tcp", TCPproxyer); err != nil {
				log.Fatal(err)
			}
		}()
	}
	fmt.Printf("Start to work\n")
	for {
		UDPproxyer.NOW = time.Now().UTC().Unix()
		time.Sleep(time.Duration(1) * time.Second)
	}
}
