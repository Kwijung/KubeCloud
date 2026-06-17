package kubevirt

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kloudia/kloudia/backend/internal/domain"
	"k8s.io/apimachinery/pkg/util/httpstream/wsstream"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
)

const consoleSubprotocol = "binary.k8s.io"

type ConsoleStreamer struct {
	namespace  string
	restConfig *rest.Config
	upgrader   websocket.Upgrader
}

func NewConsoleStreamer(namespace string, kubeconfigPath string) (*ConsoleStreamer, error) {
	cfg, err := buildRESTConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	return &ConsoleStreamer{
		namespace:  namespace,
		restConfig: cfg,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool {
				return true
			},
		},
	}, nil
}

func (s *ConsoleStreamer) StreamVirtualMachineConsole(w http.ResponseWriter, r *http.Request, vm domain.VirtualMachine) error {
	if s == nil || s.restConfig == nil {
		return fmt.Errorf("kubevirt console streamer is not configured")
	}
	upstream, err := s.dial(r, vm.ID)
	if err != nil {
		return err
	}
	client, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		_ = upstream.Close()
		return nil
	}
	defer client.Close()
	defer upstream.Close()
	proxyWebSockets(client, upstream)
	return nil
}

func (s *ConsoleStreamer) dial(r *http.Request, vmID string) (*websocket.Conn, error) {
	target, err := consoleWebSocketURL(s.restConfig.Host, s.namespace, vmID)
	if err != nil {
		return nil, err
	}
	roundTripper, holder, err := consoleRoundTripperFor(s.restConfig)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(wsstream.WebSocketProtocolHeader, consoleSubprotocol)
	resp, err := roundTripper.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	_ = resp.Body.Close()
	if holder.conn == nil {
		return nil, fmt.Errorf("console websocket connection was not established")
	}
	return holder.conn, nil
}

func consoleWebSocketURL(host string, namespace string, vmID string) (string, error) {
	parsed, err := url.Parse(host)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	default:
		return "", fmt.Errorf("unsupported kubernetes api scheme %q", parsed.Scheme)
	}
	parsed.Path = path.Join(parsed.Path, "apis", "subresources.kubevirt.io", "v1", "namespaces", namespace, "virtualmachineinstances", vmID, "console")
	parsed.RawQuery = ""
	return parsed.String(), nil
}

type consoleConnectionHolder struct {
	conn *websocket.Conn
}

type consoleWebSocketRoundTripper struct {
	tlsConfig *tls.Config
	proxy     func(*http.Request) (*url.URL, error)
	holder    *consoleConnectionHolder
}

func consoleRoundTripperFor(cfg *rest.Config) (http.RoundTripper, *consoleConnectionHolder, error) {
	transportCfg, err := cfg.TransportConfig()
	if err != nil {
		return nil, nil, err
	}
	tlsConfig, err := transport.TLSConfigFor(transportCfg)
	if err != nil {
		return nil, nil, err
	}
	proxy := cfg.Proxy
	if proxy == nil {
		proxy = utilnet.NewProxierWithNoProxyCIDR(http.ProxyFromEnvironment)
	}
	holder := &consoleConnectionHolder{}
	base := &consoleWebSocketRoundTripper{
		tlsConfig: tlsConfig,
		proxy:     proxy,
		holder:    holder,
	}
	wrapped, err := transport.HTTPWrappersForConfig(transportCfg, base)
	if err != nil {
		return nil, nil, err
	}
	return wrapped, holder, nil
}

func (rt *consoleWebSocketRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	protocols := req.Header[wsstream.WebSocketProtocolHeader]
	delete(req.Header, wsstream.WebSocketProtocolHeader)
	dialer := websocket.Dialer{
		Proxy:           rt.proxy,
		TLSClientConfig: rt.tlsConfig,
		Subprotocols:    protocols,
		ReadBufferSize:  32 * 1024,
		WriteBufferSize: 32 * 1024,
	}
	switch req.URL.Scheme {
	case "https":
		req.URL.Scheme = "wss"
	case "http":
		req.URL.Scheme = "ws"
	}
	conn, resp, err := dialer.DialContext(req.Context(), req.URL.String(), req.Header)
	if err != nil {
		if resp != nil && resp.Body != nil {
			defer resp.Body.Close()
		}
		return resp, err
	}
	rt.holder.conn = conn
	return resp, nil
}

func proxyWebSockets(client *websocket.Conn, upstream *websocket.Conn) {
	var once sync.Once
	closeBoth := func() {
		_ = client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
		_ = upstream.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
		_ = client.Close()
		_ = upstream.Close()
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		copyWebSocketMessages(client, upstream, true)
		once.Do(closeBoth)
	}()
	go func() {
		defer wg.Done()
		copyWebSocketMessages(upstream, client, false)
		once.Do(closeBoth)
	}()
	wg.Wait()
}

func copyWebSocketMessages(src *websocket.Conn, dst *websocket.Conn, forceBinary bool) {
	for {
		messageType, payload, err := src.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}
		if forceBinary {
			messageType = websocket.BinaryMessage
		}
		if err := dst.WriteMessage(messageType, payload); err != nil {
			return
		}
	}
}
