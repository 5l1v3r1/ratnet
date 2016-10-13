package https

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/awgh/bencrypt/bc"
	"github.com/awgh/ratnet/api"
)

// New : Makes a new instance of this transport module
func New(certfile string, keyfile string, node api.Node, eccMode bool) *Module {

	web := new(Module)

	web.certfile = certfile
	web.keyfile = keyfile
	web.node = node
	web.eccMode = eccMode

	web.transport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	web.client = &http.Client{Transport: web.transport}

	return web
}

// Module : HTTPS Implementation of a Transport module
type Module struct {
	transport *http.Transport
	client    *http.Client

	certfile, keyfile string
	node              api.Node

	eccMode   bool
	isRunning bool
	wg        sync.WaitGroup
	Listeners []net.Listener
}

// Name : Returns this module's common name, which should be unique
func (Module) Name() string {
	return "https"
}

// Listen : Server interface
func (h *Module) Listen(listen string, adminMode bool) {
	// make sure we are not already running
	if h.isRunning {
		log.Println("This listener is already running.")
		return
	}

	// init ssl components
	bc.InitSSL(h.certfile, h.keyfile, h.eccMode)
	cert, err := tls.LoadX509KeyPair(h.certfile, h.keyfile)
	if err != nil {
		log.Println(err.Error())
		return
	}

	// build http handler
	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.handleResponse(w, r, h.node, adminMode)
	})

	// setup Listener
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		log.Println(err.Error())
		return
	}

	// transform Listener into TLS Listener
	tlsListener := tls.NewListener(
		listener,
		&tls.Config{Certificates: []tls.Certificate{cert}},
	)

	// add Listener to the Listener pool
	h.Listeners = append(h.Listeners, listener)

	// start
	go func() {
		h.wg.Add(1)
		defer h.wg.Done()
		if err := http.Serve(tlsListener, serveMux); err != nil {
			log.Print(err.Error())
		}
	}()
	h.isRunning = true
}

func (Module) handleResponse(w http.ResponseWriter, r *http.Request, node api.Node, adminMode bool) {
	var a api.RemoteCall
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&a)
	if err != nil {
		log.Println(err.Error())
	}
	var result string
	if adminMode {
		result, err = node.AdminRPC(a.Action, a.Args...)
	} else {
		result, err = node.PublicRPC(a.Action, a.Args...)
	}
	if err != nil {
		log.Println(err.Error())
	} else if len(result) < 1 {
		result = "OK" // todo: for backwards compatability, remove when nothing needs it
	}
	w.Write([]byte(result))
}

// RPC : client interface
func (h *Module) RPC(host string, method string, args ...string) ([]byte, error) {
	var a api.RemoteCall
	a.Action = method
	a.Args = args

	b, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	//log.Println("POSTING: " + string(b))
	req, _ := http.NewRequest("POST", "https://"+host, bytes.NewReader(b))
	//req.Close = true
	req.Header.Add("Accept", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	buf, err := ioutil.ReadAll(resp.Body)
	return buf, err
}

// Stop : stops the HTTPS transport from running
func (h *Module) Stop() {
	h.isRunning = false
	for _, Listener := range h.Listeners {
		Listener.Close()
	}
	h.wg.Wait()
}
