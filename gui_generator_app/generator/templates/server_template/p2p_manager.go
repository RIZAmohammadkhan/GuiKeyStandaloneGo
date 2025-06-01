// File: GuiKeyStandaloneGo/generator/templates/server_template/p2p_manager.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	pkgCrypto "github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/pkg/crypto"
	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/pkg/p2p"
	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/pkg/types"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/routing"
	routd "github.com/libp2p/go-libp2p/p2p/discovery/routing"
)

const (
	ServerRendezvousString = "gui-key-log-service"
)

// ServerStats tracks server‐side statistics
type ServerStats struct {
	mu                    sync.RWMutex
	totalConnections      int64
	activeConnections     int64
	totalRequests         int64
	successfulRequests    int64
	failedRequests        int64
	totalEventsProcessed  int64
	lastRequestTime       time.Time
	startTime             time.Time
	advertisementFailures int64
	bootstrapFailures     int64
}

func (ss *ServerStats) RecordConnection(active bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if active {
		ss.totalConnections++
		ss.activeConnections++
	} else {
		ss.activeConnections--
	}
}

func (ss *ServerStats) RecordRequest(success bool, eventsProcessed int64) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	ss.totalRequests++
	if success {
		ss.successfulRequests++
	} else {
		ss.failedRequests++
	}
	ss.totalEventsProcessed += eventsProcessed
	ss.lastRequestTime = time.Now()
}

// ServerP2PManager manages the server’s libp2p host, DHT, and incoming streams
type ServerP2PManager struct {
	host             host.Host
	kademliaDHT      *dht.IpfsDHT
	routingDiscovery *routd.RoutingDiscovery
	logger           *log.Logger

	stats       *ServerStats
	ctx         context.Context
	cancel      context.CancelFunc
	shutdownWg  sync.WaitGroup
	streamLimit chan struct{} // to rate-limit concurrent streams

	CfgEncryptionKeyHex string

	serverLogStore LogStore // interface to your database/log storage

	internalQuit chan struct{}
}

// LogStore is an interface representing whatever storage you use
type LogStore interface {
	AddReceivedLogEvents(events []types.LogEvent, timestamp time.Time) (int, error)
}

func NewServerP2PManager(
	logger *log.Logger,
	listenAddrs []string,
	bootstrapAddrs []string,
	cfgEncryptionKeyHex string,
	store LogStore,
) (*ServerP2PManager, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "P2P_SERVER: ", log.LstdFlags|log.Lshortfile)
	}

	var opts []libp2p.Option
	opts = append(opts, libp2p.ListenAddrStrings(listenAddrs...))
	opts = append(opts, libp2p.EnableRelay())
	opts = append(opts, libp2p.EnableHolePunching())

	var kadDHT *dht.IpfsDHT
	opts = append(opts,
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			var err error
			dhtOpts := []dht.Option{dht.Mode(dht.ModeServer)}
			kadDHT, err = dht.New(context.Background(), h, dhtOpts...)
			return kadDHT, err
		}),
	)

	p2pHost, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}
	if kadDHT == nil {
		p2pHost.Close()
		return nil, fmt.Errorf("Kademlia DHT was not initialized")
	}

	ctx, cancel := context.WithCancel(context.Background())
	pm := &ServerP2PManager{
		host:                p2pHost,
		kademliaDHT:         kadDHT,
		logger:              logger,
		stats:               &ServerStats{startTime: time.Now()},
		ctx:                 ctx,
		cancel:              cancel,
		streamLimit:         make(chan struct{}, 10),
		CfgEncryptionKeyHex: cfgEncryptionKeyHex,
		serverLogStore:      store,
		internalQuit:        make(chan struct{}),
	}

	logger.Printf("Server libp2p host created with ID: %s", p2pHost.ID().String())
	return pm, nil
}

func (pm *ServerP2PManager) StartBootstrapAndDiscovery(ctx context.Context) {
	pm.logger.Println("Starting DHT bootstrap and advertisement...")

	pm.shutdownWg.Add(3)
	go pm.bootstrapManager()
	go pm.addressMonitor()
	go pm.advertisementManager()
}

func (pm *ServerP2PManager) bootstrapManager() {
	defer pm.shutdownWg.Done()
	defer pm.logger.Println("Bootstrap manager stopped")

	initialTimer := time.NewTimer(10 * time.Second)
	defer initialTimer.Stop()

	retryTicker := time.NewTicker(3 * time.Minute)
	defer retryTicker.Stop()

	firstBootstrapDone := false

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-initialTimer.C:
			if !firstBootstrapDone {
				pm.logger.Println("Server: Performing initial DHT bootstrap...")
				if err := pm.kademliaDHT.Bootstrap(pm.ctx); err != nil {
					pm.logger.Printf("Server: initial DHT bootstrap failed: %v", err)
					pm.stats.bootstrapFailures++
				} else {
					pm.logger.Println("Server: initial DHT bootstrap succeeded")
					pm.routingDiscovery = routd.NewRoutingDiscovery(pm.kademliaDHT)
				}
				firstBootstrapDone = true
			}
		case <-retryTicker.C:
			if pm.ctx.Err() != nil {
				return
			}
			pm.logger.Println("Server: Retrying DHT bootstrap...")
			if err := pm.kademliaDHT.Bootstrap(pm.ctx); err != nil {
				pm.logger.Printf("Server: periodic DHT bootstrap failed: %v", err)
				pm.stats.bootstrapFailures++
			} else {
				pm.logger.Println("Server: periodic DHT bootstrap succeeded")
			}
		}
	}
}

func (pm *ServerP2PManager) advertisementManager() {
	defer pm.shutdownWg.Done()
	defer pm.logger.Println("Advertisement manager stopped")

	for pm.routingDiscovery == nil {
		if pm.ctx.Err() != nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	for {
		pm.logger.Printf("Server: Advertising under rendezvous string %q...", ServerRendezvousString)
		// Advertise returns (peer.ID, error), so capture both and ignore the ID.
		if _, err := pm.routingDiscovery.Advertise(pm.ctx, ServerRendezvousString); err != nil {
			pm.logger.Printf("Server: advertisement failed: %v", err)
			pm.stats.advertisementFailures++
		} else {
			pm.logger.Println("Server: advertisement succeeded")
		}

		select {
		case <-time.After(15 * time.Minute):
		case <-pm.ctx.Done():
			return
		}
	}
}

func (pm *ServerP2PManager) addressMonitor() {
	defer pm.shutdownWg.Done()
	defer pm.logger.Println("Address monitor stopped")

	addrSub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated))
	if err != nil {
		pm.logger.Printf("Server: failed to subscribe to address updates: %v", err)
		return
	}
	defer addrSub.Close()

	reachabilitySub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		pm.logger.Printf("Server: failed to subscribe to reachability events: %v", err)
	} else {
		defer reachabilitySub.Close()
	}

	pm.logger.Printf("Server: Initial listen addresses: %v", pm.host.Addrs())

	for {
		select {
		case <-pm.ctx.Done():
			return
		case ev, ok := <-addrSub.Out():
			if !ok {
				if reachabilitySub == nil {
					return
				}
				addrSub = nil
				continue
			}
			addressEvent := ev.(event.EvtLocalAddressesUpdated)
			pm.logger.Printf("Server: Addresses updated. Current: %v, Diffs: %+v",
				addressEvent.Current, addressEvent.Diffs)
		case ev, ok := <-reachabilitySub.Out():
			if !ok {
				if addrSub == nil {
					return
				}
				reachabilitySub = nil
				continue
			}
			reachabilityEvent := ev.(event.EvtLocalReachabilityChanged)
			pm.logger.Printf("Server: Reachability changed: %s",
				reachabilityEvent.Reachability.String())
		}
	}
}

// handleIncomingLogStream is invoked on each new stream opened by a client over ProtocolID.
func (pm *ServerP2PManager) handleIncomingLogStream(stream network.Stream) {
	remotePeer := stream.Conn().RemotePeer()
	pm.logger.Printf("Server: Received new log stream from %s", remotePeer.String())

	pm.stats.RecordConnection(true)
	defer func() {
		pm.stats.RecordConnection(false)
		stream.Reset()
		pm.logger.Printf("Server: Closed log stream from %s", remotePeer.String())
	}()

	select {
	case pm.streamLimit <- struct{}{}:
		defer func() { <-pm.streamLimit }()
	case <-pm.ctx.Done():
		return
	}

	_ = stream.SetReadDeadline(time.Now().Add(60 * time.Second))
	_ = stream.SetWriteDeadline(time.Now().Add(30 * time.Second))

	var request p2p.LogBatchRequest
	if err := p2p.ReadMessage(stream, &request); err != nil {
		if request.Header.RequestID != "" {
			_ = p2p.SendErrorResponse(stream, request.Header.RequestID,
				"invalid_request", "failed to read or validate LogBatchRequest", false)
		}
		if err != io.EOF {
			pm.logger.Printf("Server: Error reading LogBatchRequest from %s: %v", remotePeer, err)
		} else {
			pm.logger.Printf("Server: Stream closed by client %s before full request (EOF)", remotePeer)
		}
		pm.stats.RecordRequest(false, 0)
		return
	}
	pm.logger.Printf("Server: Received LogBatchRequest (RequestID=%s) from ClientAppID=%s, PayloadSize=%d bytes",
		request.Header.RequestID, request.AppClientID, len(request.EncryptedLogPayload))

	decryptedJSON, err := pkgCrypto.Decrypt(request.EncryptedLogPayload, pm.CfgEncryptionKeyHex)
	if err != nil {
		pm.logger.Printf("Server: Failed to decrypt payload from %s (ClientAppID=%s): %v",
			remotePeer, request.AppClientID, err)
		_ = p2p.SendErrorResponse(stream, request.Header.RequestID,
			"decryption_failed", "Failed to decrypt payload", false)
		pm.stats.RecordRequest(false, 0)
		return
	}

	var logEvents []types.LogEvent
	if err := json.Unmarshal(decryptedJSON, &logEvents); err != nil {
		pm.logger.Printf("Server: Failed to unmarshal JSON from %s (ClientAppID=%s): %v",
			remotePeer, request.AppClientID, err)
		_ = p2p.SendErrorResponse(stream, request.Header.RequestID,
			"invalid_payload", "JSON unmarshal failed", false)
		pm.stats.RecordRequest(false, 0)
		return
	}
	pm.logger.Printf("Server: Unmarshaled %d log events from ClientAppID=%s", len(logEvents), request.AppClientID)

	processedCount, err := pm.serverLogStore.AddReceivedLogEvents(logEvents, time.Now().UTC())
	if err != nil {
		pm.logger.Printf("Server: Failed to store log events from %s (ClientAppID=%s): %v",
			remotePeer, request.AppClientID, err)
		_ = p2p.SendErrorResponse(stream, request.Header.RequestID,
			"db_error", fmt.Sprintf("Server DB error: %v", err), true)
		pm.stats.RecordRequest(false, int64(processedCount))
		return
	}

	if err := p2p.SendLogBatchResponse(stream,
		request.Header.RequestID,
		"success",
		fmt.Sprintf("Successfully processed %d log events.", processedCount),
		processedCount,
		0,
	); err != nil {
		pm.logger.Printf("Server: Failed to send success response to %s: %v", remotePeer, err)
		pm.stats.RecordRequest(false, int64(processedCount))
	} else {
		pm.logger.Printf("Server: Sent success response (RequestID=%s) to %s for %d events (ClientAppID=%s)",
			request.Header.RequestID, remotePeer, processedCount, request.AppClientID)
		pm.stats.RecordRequest(true, int64(processedCount))
	}
}

func (pm *ServerP2PManager) RegisterStreamHandler() {
	pm.host.SetStreamHandler(p2p.ProtocolID, pm.handleIncomingLogStream)
}

func (pm *ServerP2PManager) Close() error {
	pm.logger.Println("Server: Shutting down P2P Manager...")

	pm.cancel()

	done := make(chan struct{})
	go func() {
		pm.shutdownWg.Wait()
		close(done)
	}()
	select {
	case <-done:
		pm.logger.Println("Server: Background goroutines stopped gracefully")
	case <-time.After(10 * time.Second):
		pm.logger.Println("Server: Timeout waiting for background goroutines")
	}

	if pm.kademliaDHT != nil {
		if err := pm.kademliaDHT.Close(); err != nil {
			pm.logger.Printf("Server: Error closing Kademlia DHT: %v", err)
		}
	}

	if pm.host != nil {
		pm.logger.Println("Server: Closing libp2p host...")
		return pm.host.Close()
	}
	return nil
}

func runServerP2PManager(p2pManager *ServerP2PManager, internalQuit <-chan struct{}, wg *sync.WaitGroup, logger *log.Logger) {
	defer wg.Done()
	p2pManager.logger.Println("Server P2P Manager goroutine starting.")

	p2pCtx, cancelP2pCtx := context.WithCancel(context.Background())
	defer cancelP2pCtx()

	p2pManager.StartBootstrapAndDiscovery(p2pCtx)
	p2pManager.RegisterStreamHandler()

	<-internalQuit
	p2pManager.logger.Println("Server P2P Manager goroutine stopping...")
}
