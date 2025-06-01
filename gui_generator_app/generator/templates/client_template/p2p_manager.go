// File: GuiKeyStandaloneGo/generator/templates/client_template/p2p_manager.go
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/pkg/p2p"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	routd "github.com/libp2p/go-libp2p/p2p/discovery/routing"
)

const (
	// peerstoreTTL is used when caching the server's addresses in the peerstore.
	peerstoreTTL = 2 * time.Hour
)

// ConnectionState represents the current connection state
type ConnectionState int32

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateFailed
)

// ConnectionStats tracks connection statistics
type ConnectionStats struct {
	mu                  sync.RWMutex
	totalAttempts       int64
	successfulAttempts  int64
	failedAttempts      int64
	lastSuccess         time.Time
	lastFailure         time.Time
	consecutiveFailures int64
}

func (cs *ConnectionStats) RecordAttempt(success bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.totalAttempts++
	if success {
		cs.successfulAttempts++
		cs.lastSuccess = time.Now()
		cs.consecutiveFailures = 0
	} else {
		cs.failedAttempts++
		cs.lastFailure = time.Now()
		cs.consecutiveFailures++
	}
}

func (cs *ConnectionStats) GetStats() (total, success, failed, consecutive int64, lastSuccessTime, lastFailureTime time.Time) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.totalAttempts, cs.successfulAttempts, cs.failedAttempts,
		cs.consecutiveFailures, cs.lastSuccess, cs.lastFailure
}

// BackoffManager handles exponential backoff with jitter
type BackoffManager struct {
	baseDelay     time.Duration
	maxDelay      time.Duration
	backoffFactor float64
	jitterFactor  float64
	attempts      int64 // only increment on genuine failures
}

func NewBackoffManager() *BackoffManager {
	return &BackoffManager{
		baseDelay:     1 * time.Second,
		maxDelay:      5 * time.Minute,
		backoffFactor: 2.0,
		jitterFactor:  0.1,
	}
}

// Failure returns the next delay after a genuine failure (and increments attempts).
func (bm *BackoffManager) Failure() time.Duration {
	idx := atomic.AddInt64(&bm.attempts, 1) - 1
	delay := float64(bm.baseDelay) * math.Pow(bm.backoffFactor, float64(idx))
	if delay > float64(bm.maxDelay) {
		delay = float64(bm.maxDelay)
	}
	jitter := delay * bm.jitterFactor * (2*rand.Float64() - 1)
	return time.Duration(delay + jitter)
}

func (bm *BackoffManager) Reset() {
	atomic.StoreInt64(&bm.attempts, 0)
}

// StreamPool manages reusable streams to reduce connection overhead
type StreamPool struct {
	mu      sync.RWMutex
	streams map[peer.ID]*pooledStream
	maxAge  time.Duration
}

type pooledStream struct {
	stream    network.Stream
	createdAt time.Time
	inUse     bool
}

func NewStreamPool() *StreamPool {
	return &StreamPool{
		streams: make(map[peer.ID]*pooledStream),
		maxAge:  10 * time.Minute,
	}
}

func (sp *StreamPool) Get(peerID peer.ID) network.Stream {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if ps, exists := sp.streams[peerID]; exists {
		// Drop if closed or expired
		if ps.stream == nil || ps.stream.Conn().IsClosed() || time.Since(ps.createdAt) >= sp.maxAge {
			ps.stream.Reset()
			delete(sp.streams, peerID)
			return nil
		}
		if !ps.inUse {
			ps.inUse = true
			return ps.stream
		}
		return nil
	}
	return nil
}

func (sp *StreamPool) Put(peerID peer.ID, stream network.Stream) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if stream == nil || stream.Conn().IsClosed() {
		return
	}
	sp.streams[peerID] = &pooledStream{
		stream:    stream,
		createdAt: time.Now(),
		inUse:     false,
	}
}

func (sp *StreamPool) Remove(peerID peer.ID) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if ps, exists := sp.streams[peerID]; exists {
		if ps.stream != nil {
			ps.stream.Reset()
		}
		delete(sp.streams, peerID)
	}
}

func (sp *StreamPool) Cleanup() {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	for peerID, ps := range sp.streams {
		if time.Since(ps.createdAt) > sp.maxAge || ps.stream.Conn().IsClosed() {
			if ps.stream != nil {
				ps.stream.Reset()
			}
			delete(sp.streams, peerID)
		}
	}
}

// P2PManager manages the client’s libp2p host, DHT, and outgoing streams to the server.
type P2PManager struct {
	host               host.Host
	kademliaDHT        *dht.IpfsDHT
	routingDiscovery   *routd.RoutingDiscovery
	logger             *log.Logger
	serverPeerIDStr    string
	bootstrapAddrInfos []peer.AddrInfo

	connectionState     atomic.Int32 // use atomic.Int32 for ConnectionState
	connectionStats     *ConnectionStats
	backoffManager      *BackoffManager
	streamPool          *StreamPool
	ctx                 context.Context
	cancel              context.CancelFunc
	shutdownWg          sync.WaitGroup
	lastServerContact   time.Time
	healthCheckInterval time.Duration
	rateLimiter         chan struct{} // limit concurrent SendLogBatch
}

func NewP2PManager(logger *log.Logger, serverPeerID string, bootstrapAddrs []string) (*P2PManager, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "P2P_CLIENT: ", log.LstdFlags|log.Lshortfile)
	}

	var bootAddrsInfo []peer.AddrInfo
	for _, addrStr := range bootstrapAddrs {
		if addrStr == "" {
			continue
		}
		ai, err := peer.AddrInfoFromString(addrStr)
		if err != nil {
			logger.Printf("WARN: Invalid bootstrap address '%s': %v", addrStr, err)
			continue
		}
		bootAddrsInfo = append(bootAddrsInfo, *ai)
	}

	if len(bootAddrsInfo) == 0 {
		logger.Println("WARN: No valid bootstrap addresses; DHT functionality limited")
	}

	var opts []libp2p.Option
	opts = append(opts,
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
			"/ip4/0.0.0.0/udp/0/quic-v1",
		),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
	)

	if len(bootAddrsInfo) > 0 {
		opts = append(opts, libp2p.EnableAutoRelayWithStaticRelays(bootAddrsInfo))
	} else {
		opts = append(opts, libp2p.EnableAutoRelay())
	}

	var kadDHT *dht.IpfsDHT
	opts = append(opts,
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			var err error
			dhtOpts := []dht.Option{dht.Mode(dht.ModeClient)}
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
	pm := &P2PManager{
		host:                p2pHost,
		kademliaDHT:         kadDHT,
		logger:              logger,
		serverPeerIDStr:     serverPeerID,
		bootstrapAddrInfos:  bootAddrsInfo,
		connectionStats:     &ConnectionStats{},
		backoffManager:      NewBackoffManager(),
		streamPool:          NewStreamPool(),
		ctx:                 ctx,
		cancel:              cancel,
		healthCheckInterval: 30 * time.Second,
		rateLimiter:         make(chan struct{}, 10),
	}
	pm.connectionState.Store(int32(StateDisconnected))

	logger.Printf("Client libp2p host created with ID: %s", p2pHost.ID().String())
	return pm, nil
}

func (pm *P2PManager) StartDiscoveryAndBootstrap(ctx context.Context) {
	pm.logger.Println("Client: Starting discovery and bootstrap...")

	if len(pm.bootstrapAddrInfos) == 0 {
		pm.logger.Println("Client: No bootstrap peers; DHT will be limited")
		return
	}

	pm.shutdownWg.Add(4)
	go pm.bootstrapManager()
	go pm.serverConnectionManager()
	go pm.addressMonitor()
	go pm.cleanupManager()

	// Initial attempt to connect to any bootstrap peers
	pm.connectToBootstrapPeers(ctx)
}

func (pm *P2PManager) bootstrapManager() {
	defer pm.shutdownWg.Done()
	defer pm.logger.Println("Client: Bootstrap manager stopped")

	initialTimer := time.NewTimer(30 * time.Second)
	defer initialTimer.Stop()

	retryTicker := time.NewTicker(3 * time.Minute)
	defer retryTicker.Stop()

	dhtTicker := time.NewTicker(15 * time.Minute)
	defer dhtTicker.Stop()

	firstBootstrapDone := false

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-initialTimer.C:
			if !firstBootstrapDone {
				pm.logger.Println("Client: Performing initial DHT bootstrap...")
				if err := pm.kademliaDHT.Bootstrap(pm.ctx); err != nil {
					pm.logger.Printf("Client: initial DHT bootstrap failed: %v", err)
				} else {
					pm.logger.Println("Client: initial DHT bootstrap succeeded")
					pm.routingDiscovery = routd.NewRoutingDiscovery(pm.kademliaDHT)
				}
				firstBootstrapDone = true
			}
		case <-retryTicker.C:
			if pm.ctx.Err() != nil {
				return
			}
			pm.logger.Println("Client: Retrying bootstrap peer connections...")
			pm.connectToBootstrapPeers(pm.ctx)
		case <-dhtTicker.C:
			if pm.ctx.Err() != nil {
				return
			}
			pm.logger.Println("Client: Performing periodic DHT bootstrap...")
			if err := pm.kademliaDHT.Bootstrap(pm.ctx); err != nil {
				pm.logger.Printf("Client: periodic DHT bootstrap failed: %v", err)
			}
		}
	}
}

func (pm *P2PManager) connectToBootstrapPeers(ctx context.Context) {
	if len(pm.bootstrapAddrInfos) == 0 {
		return
	}
	pm.logger.Printf("Client: Attempting to connect to %d bootstrap peers...", len(pm.bootstrapAddrInfos))

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 5) // limit concurrent dials

	for _, pi := range pm.bootstrapAddrInfos {
		if pm.host.Network().Connectedness(pi.ID) == network.Connected {
			continue
		}
		wg.Add(1)
		go func(peerInfo peer.AddrInfo) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := pm.host.Connect(connectCtx, peerInfo); err != nil {
				if ctx.Err() == nil {
					pm.logger.Printf("Client: failed to connect to bootstrap peer %s: %v", peerInfo.ID, err)
				}
			} else {
				pm.logger.Printf("Client: successfully connected to bootstrap peer: %s", peerInfo.ID.String())
			}
		}(pi)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Minute):
		pm.logger.Println("Client: bootstrap connection attempts timed out")
	case <-ctx.Done():
		return
	}
}

func (pm *P2PManager) serverConnectionManager() {
	defer pm.shutdownWg.Done()
	defer pm.logger.Println("Client: Server connection manager stopped")

	if pm.serverPeerIDStr == "" {
		pm.logger.Println("Client: No server PeerID configured")
		return
	}

	targetPeerID, err := peer.Decode(pm.serverPeerIDStr)
	if err != nil {
		pm.logger.Printf("Client: invalid server PeerID: %v", err)
		return
	}

	ticker := time.NewTicker(pm.healthCheckInterval)
	defer ticker.Stop()

	// Initial connection attempt
	pm.findAndConnectToServer(pm.ctx, targetPeerID)

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-ticker.C:
			if pm.shouldAttemptConnection(targetPeerID) {
				pm.findAndConnectToServer(pm.ctx, targetPeerID)
			}
		}
	}
}

func (pm *P2PManager) shouldAttemptConnection(targetPeerID peer.ID) bool {
	// If already connected and last contact was recent, skip.
	if pm.host.Network().Connectedness(targetPeerID) == network.Connected {
		if time.Since(pm.lastServerContact) < pm.healthCheckInterval*2 {
			return false
		}
	}

	// Check backoff
	_, _, _, consecutive, _, lastFailure := pm.connectionStats.GetStats()
	if consecutive > 0 {
		backoffDelay := pm.backoffManager.Failure() // get delay for last failure
		if time.Since(lastFailure) < backoffDelay {
			return false
		}
	}

	return true
}

func (pm *P2PManager) findAndConnectToServer(ctx context.Context, targetPeerID peer.ID) {
	currentState := ConnectionState(pm.connectionState.Load())
	if currentState == StateConnecting {
		return
	}

	pm.connectionState.Store(int32(StateConnecting))
	pm.logger.Printf("Client: Attempting to find and connect to server %s...", targetPeerID)

	// 1) Try direct connection if addresses cached
	if addrs := pm.host.Peerstore().Addrs(targetPeerID); len(addrs) > 0 {
		connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := pm.host.Connect(connectCtx, peer.AddrInfo{ID: targetPeerID, Addrs: addrs}); err == nil {
			pm.onConnectionSuccess(targetPeerID)
			return
		}
	}

	// 2) Fall back to DHT lookup
	findCtx, findCancel := context.WithTimeout(ctx, 45*time.Second)
	defer findCancel()

	peerInfo, err := pm.kademliaDHT.FindPeer(findCtx, targetPeerID)
	if err != nil {
		pm.onConnectionFailure(targetPeerID, fmt.Errorf("DHT find peer failed: %w", err))
		return
	}
	pm.logger.Printf("Client: Found server %s via DHT at addrs: %v", targetPeerID, peerInfo.Addrs)

	pm.host.Peerstore().AddAddrs(peerInfo.ID, peerInfo.Addrs, peerstoreTTL)

	connectCtx, connectCancel := context.WithTimeout(ctx, 30*time.Second)
	defer connectCancel()
	if err := pm.host.Connect(connectCtx, peerInfo); err != nil {
		pm.onConnectionFailure(targetPeerID, fmt.Errorf("dial failed: %w", err))
		return
	}
	pm.onConnectionSuccess(targetPeerID)
}

func (pm *P2PManager) onConnectionSuccess(peerID peer.ID) {
	pm.connectionState.Store(int32(StateConnected))
	pm.connectionStats.RecordAttempt(true)
	pm.backoffManager.Reset()

	pm.lastServerContact = time.Now()
	pm.logger.Printf("Client: Successfully connected to server %s", peerID)
}

func (pm *P2PManager) onConnectionFailure(peerID peer.ID, err error) {
	pm.connectionState.Store(int32(StateFailed))
	delay := pm.backoffManager.Failure()
	pm.connectionStats.RecordAttempt(false)

	if pm.ctx.Err() == nil {
		pm.logger.Printf("Client: Failed to connect to server %s: %v (backoff %s)", peerID, err, delay)
	}
}

func (pm *P2PManager) addressMonitor() {
	defer pm.shutdownWg.Done()
	defer pm.logger.Println("Client: Address monitor stopped")

	addrSub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated))
	if err != nil {
		pm.logger.Printf("Client: failed to subscribe to address updates: %v", err)
		return
	}
	defer addrSub.Close()

	reachabilitySub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		pm.logger.Printf("Client: failed to subscribe to reachability events: %v", err)
	} else {
		defer reachabilitySub.Close()
	}

	pm.logger.Printf("Client: Initial listen addresses: %v", pm.host.Addrs())

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
			pm.logger.Printf("Client: Addresses updated. Current: %v, Diffs: %+v",
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
			pm.logger.Printf("Client: Reachability changed: %s",
				reachabilityEvent.Reachability.String())
		}
	}
}

func (pm *P2PManager) cleanupManager() {
	defer pm.shutdownWg.Done()
	defer pm.logger.Println("Client: Cleanup manager stopped")

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-ticker.C:
			pm.streamPool.Cleanup()
		}
	}
}

// SendLogBatch sends client’s log batch to server, reusing a pooled stream if possible.
// It always checks that resp.Header.RequestID == req.Header.RequestID.
func (pm *P2PManager) SendLogBatch(ctx context.Context, clientAppID string, encryptedPayload []byte) (*p2p.LogBatchResponse, error) {
	// Rate limiting
	select {
	case pm.rateLimiter <- struct{}{}:
		defer func() { <-pm.rateLimiter }()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("rate limit exceeded: too many concurrent requests")
	}

	if pm.serverPeerIDStr == "" {
		return nil, fmt.Errorf("server PeerID not configured")
	}
	targetPeerID, err := peer.Decode(pm.serverPeerIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid server PeerID '%s': %w", pm.serverPeerIDStr, err)
	}

	// Check if connected
	if pm.host.Network().Connectedness(targetPeerID) != network.Connected {
		return nil, fmt.Errorf("not connected to server %s", targetPeerID)
	}

	// Try pooled stream first
	stream := pm.streamPool.Get(targetPeerID)
	if stream == nil {
		streamCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		stream, err = pm.host.NewStream(streamCtx, targetPeerID, p2p.ProtocolID)
		if err != nil {
			pm.streamPool.Remove(targetPeerID)
			return nil, fmt.Errorf("failed to open new stream to server %s: %w", targetPeerID, err)
		}
		pm.logger.Printf("Client: Created new stream to server %s", targetPeerID)
	}

	// Ensure stream is returned to pool if healthy, otherwise closed
	defer func() {
		if stream != nil && stream.Conn() != nil && !stream.Conn().IsClosed() {
			pm.streamPool.Put(targetPeerID, stream)
		} else if stream != nil {
			stream.Reset()
		}
	}()

	// Build and send request
	req := p2p.LogBatchRequest{
		Header:              p2p.NewMessageHeader(p2p.MessageTypeLogBatch),
		AppClientID:         clientAppID,
		EncryptedLogPayload: encryptedPayload,
	}
	if err := p2p.WriteMessageWithTimeout(stream, &req, 30*time.Second); err != nil {
		return nil, fmt.Errorf("failed to write LogBatchRequest: %w", err)
	}

	// Read response
	var resp p2p.LogBatchResponse
	if err := p2p.ReadMessageWithTimeout(stream, &resp, 60*time.Second); err != nil {
		return nil, fmt.Errorf("failed to read LogBatchResponse: %w", err)
	}

	// Validate message type
	if resp.Header.MessageType != p2p.MessageTypeLogBatch && resp.Header.MessageType != p2p.MessageTypeError {
		return nil, fmt.Errorf("unexpected response message type: %s", resp.Header.MessageType)
	}

	// Validate RequestID matches
	if resp.Header.RequestID != req.Header.RequestID {
		return nil, fmt.Errorf("response RequestID mismatch: expected %s, got %s",
			req.Header.RequestID, resp.Header.RequestID)
	}

	return &resp, nil
}

// GetConnectionStats returns the current ConnectionState and stats object.
func (pm *P2PManager) GetConnectionStats() (ConnectionState, *ConnectionStats) {
	state := ConnectionState(pm.connectionState.Load())
	return state, pm.connectionStats
}

// Close gracefully shuts down background goroutines, DHT, and host.
func (pm *P2PManager) Close() error {
	pm.logger.Println("Client: Shutting down P2P Manager...")

	// Cancel context to stop everything
	pm.cancel()

	// Wait for background goroutines
	done := make(chan struct{})
	go func() {
		pm.shutdownWg.Wait()
		close(done)
	}()
	select {
	case <-done:
		pm.logger.Println("Client: Background goroutines stopped")
	case <-time.After(10 * time.Second):
		pm.logger.Println("Client: Timeout waiting for background goroutines")
	}

	// Clean up stream pool
	pm.streamPool.Cleanup()

	// Close DHT
	if pm.kademliaDHT != nil {
		if err := pm.kademliaDHT.Close(); err != nil {
			pm.logger.Printf("Client: Error closing DHT: %v", err)
		}
	}

	// Close host
	if pm.host != nil {
		pm.logger.Println("Client: Closing libp2p host...")
		return pm.host.Close()
	}
	return nil
}

// runP2PManager is a helper if you spawn the P2P manager in its own goroutine.
func runP2PManager(p2pManager *P2PManager, internalQuit <-chan struct{}, wg *sync.WaitGroup, logger *log.Logger) {
	defer wg.Done()
	p2pManager.logger.Println("Client P2P Manager goroutine starting.")

	p2pCtx, cancelP2pCtx := context.WithCancel(context.Background())
	defer cancelP2pCtx()

	p2pManager.StartDiscoveryAndBootstrap(p2pCtx)

	<-internalQuit
	p2pManager.logger.Println("Client P2P Manager goroutine stopping...")
}
