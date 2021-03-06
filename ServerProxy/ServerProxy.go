package main

import (
	"errors"
	//	"bytes"
	"flag"
	"github.com/miekg/dns"
	//"dns-master"
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

// called for debug output
func _D(fmt string, v ...interface{}) {
	if DEBUG {
		log.Printf(fmt, v...)
	}
}

type Server struct {
	ACCESS      []*net.IPNet
	SERVERS     []string
	s_len       int
	entries     int64
	max_entries int64
	NOW         int64
	giant       *sync.RWMutex
	timeout     time.Duration
	TransPro    int    //specify for transmit protocol
	Port        string //the serving port of resolver
}

const UDPcode = 1
const TCPcode = 2

func (this Server) DoDNSquery(m dns.Msg, TransProString string, server []string, timeout time.Duration) (*dns.Msg, error) {
	dnsClient := new(dns.Client)
	if dnsClient == nil {
		return nil, errors.New("Cannot create DNS client")
	}

	dnsClient.ReadTimeout = timeout
	dnsClient.WriteTimeout = timeout
	if TransProString != "TCP" && TransProString != "UDP" {
		return nil, errors.New("TransProString run")
	}
	dnsClient.Net = strings.ToLower(TransProString)
	ServerStr := server[rand.Intn(len(server))]
	ServerAddr := net.ParseIP(ServerStr)
	if ServerAddr.To16() != nil {
		ServerStr = "[" + ServerStr + "]:" + this.Port
	} else if ServerAddr.To4() != nil {
		ServerStr = ServerStr + this.Port
	} else {
		return nil, errors.New("invalid Server Address")
	}
	dnsResponse, _, err := dnsClient.Exchange(&m, ServerStr)
	if err != nil {
		return nil, err
	}
	return dnsResponse, nil
}

//not sure how to make a server fail, error 501?
func (this Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	TransProString := r.Header.Get("Proxy-DNS-Transport")
	if TransProString == "TCP" {
		this.TransPro = TCPcode
	} else if TransProString == "UDP" {
		this.TransPro = UDPcode
	} else {
		_D("Transport protol not udp or tcp")
		http.Error(w, "Server Error: unknown transport protocol", 415)
		return
	}
	contentTypeStr := r.Header.Get("Content-Type")
	if contentTypeStr != "application/octet-stream" {
		_D("Content-Type illegal")
		http.Error(w, "Server Error: unknown content type", 415)
		return
	}
	var requestBody []byte
	requestBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Server Error: error in reading request", 400)
		_D("error in reading HTTP request, error message: %s", err)
		return
	}
	if len(requestBody) < (int)(r.ContentLength) {
		http.Error(w, "Server Error: error in reading request", 400)
		_D("fail to read all HTTP content")
		return
	}
	var dnsRequest dns.Msg
	err = dnsRequest.Unpack(requestBody)
	if err != nil {
		http.Error(w, "Server Error: bad DNS request", 400)
		_D("error in packing HTTP response to DNS, error message: %s", err)
		return
	}
	/*
		dnsClient := new(dns.Client)
		if dnsClient == nil {
			http.Error(w, "Server Error", 500)
			_D("cannot create DNS client")
			return
		}
		dnsClient.ReadTimeout = this.timeout
		dnsClient.WriteTimeout = this.timeout
		dnsClient.Net = TransProString
		//will use a parameter to let user address resolver in future
		dnsResponse, RTT, err := dnsClient.Exchange(&dnsRequest, this.SERVERS[rand.Intn(len(this.SERVERS))])
		//dnsResponse, RTT, err := dnsClient.Exchange(&dnsRequest, this.SERVERS[0])
		if err != nil {
			_D("error in communicate with resolver, error message: %s", err)
			http.Error(w, "Server Error", 500)
			return
		} else {
			_D("request took %s", RTT)
		}
		if dnsResponse == nil {
			_D("no response back")
			http.Error(w, "Server Error:No Recursive response", 500)
			return
		}*/
	dnsResponse, err := this.DoDNSquery(dnsRequest, TransProString, this.SERVERS, this.timeout)
	if err != nil {
		_D("error in communicate with resolver, error message: %s", err)
		http.Error(w, err.Error(), 500)
		return
	}
	if dnsResponse == nil {
		_D("no response back")
		http.Error(w, "Server Error:No Recursive response", 500)
		return
	}
	response_bytes, err := dnsResponse.Pack()
	if err != nil {
		http.Error(w, "Server Error: error packing reply", 500)
		_D("error in packing request, error message: %s", err)
		return
	}
	_, err = w.Write(response_bytes)
	if err != nil {
		_D("Can not write response rightly, error message: %s", err)
		return
	}
	//don't know how to creat a response here
}

func main() {
	var (
		S_SERVERS     string
		timeout       int
		max_entries   int64
		ACCESS        string
		ServeTLS      bool
		tls_cert_path string
		tls_key_path  string
		port          string
		S_Listening   string
	)
	flag.StringVar(&S_SERVERS, "proxy", "127.0.0.1", "we proxy requests to those servers") //Not sure use IP or URL, default server undefined
	flag.IntVar(&timeout, "timeout", 5, "timeout")
	flag.BoolVar(&DEBUG, "debug", false, "enable/disable debug")
	flag.Int64Var(&max_entries, "max_cache_entries", 2000000, "max cache entries")
	flag.StringVar(&ACCESS, "access", "0.0.0.0/0", "allow those networks, use 0.0.0.0/0 to allow everything")
	flag.BoolVar(&ServeTLS, "ServeTls", false, "whether serve TLS")
	flag.StringVar(&tls_cert_path, "certificate_path", "", "the path of server's certicate for TLS")
	flag.StringVar(&tls_key_path, "key_path", "", "the path of server's key for TLS")
	flag.StringVar(&port, "p", "53", "the serving port of DNS server we forward to, defalut 53")
	flag.StringVar(&S_Listening, "listen", "", "the address the serverProxy is listening, split with comma")
	flag.Parse()
	servers := strings.Split(S_SERVERS, ",")
	proxyServer := Server{
		SERVERS:     servers,
		timeout:     time.Duration(timeout) * time.Second,
		max_entries: max_entries,
		ACCESS:      make([]*net.IPNet, 0),
		Port:        port}
	for _, mask := range strings.Split(ACCESS, ",") {
		_, cidr, err := net.ParseCIDR(mask)
		if err != nil {
			panic(err)
		}
		_D("added access for %s\n", mask)
		proxyServer.ACCESS = append(proxyServer.ACCESS, cidr)
	}
	_D("start server HTTP")
	for _, ipaddress := range strings.Split(S_Listening, ",") {
		err := http.ListenAndServe(ipaddress+":80", proxyServer)
		if err != nil {
			log.Fatal("ListenAndServe:", err)
			return
		}
		if ServeTLS {
			_D("startTLS")
			err := http.ListenAndServeTLS(ipaddress+":443", tls_cert_path, tls_key_path, proxyServer)
			_D("ListenAndServe:", err)
			if err != nil {
				log.Fatal("ListenAndServe:", err)
				return
			}
		}
	}

	for {
		proxyServer.NOW = time.Now().UTC().Unix()
		time.Sleep(time.Duration(1) * time.Second)
	}
}
