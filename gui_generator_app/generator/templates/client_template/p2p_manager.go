package main

import (
	"context"
	"fmt"
	"log" // Kept for Min in shouldAttemptConnection if you decide to use consecutiveFailures there, but not strictly needed for the 2-min fixed delay
	// No longer needed for BackoffManager, but might be used elsewhere
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

const ServerRendezvousString = "guikey-standalone-logserver/v1.0.0"

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

// BackoffManager handles fixed delay retries.
type BackoffManager struct {
	fixedDelay time.Duration
}

// NewBackoffManager creates a BackoffManager with a fixed 2-minute delay.
func NewBackoffManager() *BackoffManager {
	return &BackoffManager{
		fixedDelay: 2 * time.Minute,
	}
}

// GetDelay returns the configured fixed delay.
func (bm *BackoffManager) GetDelay() time.Duration {
	return bm.fixedDelay
}

// StreamPool manages reusable streams to reduce connection overhead
// NOTE: This stream pool is NOT used by SendLogBatch due to server's stream handling,
// but kept for potential future use with other protocols.
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
			if ps.stream != nil { // Check before calling Reset
				ps.stream.Reset()
			}
			delete(sp.streams, peerID)
			return nil
		}
		if !ps.inUse {
			ps.inUse = true
			return ps.stream
		}
		return nil // Stream exists but is in use
	}
	return nil // Stream does not exist for this peer
}

func (sp *StreamPool) Put(peerID peer.ID, stream network.Stream) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if stream == nil || stream.Conn().IsClosed() {
		return // Don't pool closed streams
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
		if ps.stream == nil || time.Since(ps.createdAt) > sp.maxAge || ps.stream.Conn().IsClosed() {
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
	streamPool          *StreamPool // Kept for general purpose, but not for SendLogBatch
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
		backoffManager:      NewBackoffManager(), // Use new BackoffManager
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

// findServerViaRendezvous attempts to find the server using the DHT's routing discovery.
func (pm *P2PManager) findServerViaRendezvous(ctx context.Context, targetPeerID peer.ID) bool {
	if pm.routingDiscovery == nil {
		pm.logger.Println("Client: RoutingDiscovery not initialized, cannot use rendezvous")
		return false
	}

	pm.logger.Printf("Client: Searching for servers via rendezvous: %s", ServerRendezvousString)

	discoverCtx, cancelDiscover := context.WithTimeout(ctx, 30*time.Second)
	defer cancelDiscover()

	peerChan, err := pm.routingDiscovery.FindPeers(discoverCtx, ServerRendezvousString)
	if err != nil {
		pm.logger.Printf("Client: Failed to start peer discovery: %v", err)
		return false
	}

	foundTarget := false
	peersFound := 0
	for {
		select {
		case <-discoverCtx.Done():
			pm.logger.Printf("Client: Rendezvous discovery timed out. Found %d peers, target found: %v", peersFound, foundTarget)
			return foundTarget
		case pi, ok := <-peerChan:
			if !ok { // Channel closed
				pm.logger.Printf("Client: Rendezvous discovery completed. Found %d peers, target found: %v", peersFound, foundTarget)
				return foundTarget
			}
			if pi.ID == "" { // Empty peer info, skip
				continue
			}
			peersFound++
			pm.logger.Printf("Client: Found peer via rendezvous: %s (%d addrs)", pi.ID, len(pi.Addrs))

			if pi.ID == targetPeerID {
				pm.logger.Printf("Client: Found target server %s via rendezvous!", targetPeerID)
				if len(pi.Addrs) > 0 {
					pm.host.Peerstore().AddAddrs(pi.ID, pi.Addrs, peerstoreTTL)
					pm.logger.Printf("Client: Added/updated addresses for target server %s in peerstore.", targetPeerID)

					connectCtx, connectCancel := context.WithTimeout(ctx, 15*time.Second)
					if err := pm.host.Connect(connectCtx, pi); err == nil {
						pm.logger.Printf("Client: Successfully connected to server %s found via rendezvous.", targetPeerID)
						foundTarget = true
					} else {
						pm.logger.Printf("Client: Failed to connect to server %s found via rendezvous: %v", targetPeerID, err)
					}
					connectCancel()
					if foundTarget {
						return true
					}
				} else {
					pm.logger.Printf("Client: Target server %s found via rendezvous but no addresses advertised with it.", targetPeerID)
				}
			}
		}
	}
}
func (pm *P2PManager) StartDiscoveryAndBootstrap(ctx context.Context) {
	pm.logger.Println("Client: Starting discovery and bootstrap...")

	if len(pm.bootstrapAddrInfos) == 0 {
		pm.logger.Println("Client: No bootstrap peers; DHT will be limited")
	}

	pm.connectToBootstrapPeers(ctx)

	pm.shutdownWg.Add(4)
	go pm.bootstrapManager()
	go pm.serverConnectionManager()
	go pm.addressMonitor()
	go pm.cleanupManager()
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
					pm.logger.Printf("Client: Initial DHT bootstrap failed: %v", err)
				} else {
					pm.logger.Println("Client: Initial DHT bootstrap process completed/initiated.")
					if pm.routingDiscovery == nil {
						pm.routingDiscovery = routd.NewRoutingDiscovery(pm.kademliaDHT)
						pm.logger.Println("Client: RoutingDiscovery initialized.")
					}
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
			pm.logger.Println("Client: Performing periodic DHT bootstrap refresh...")
			if err := pm.kademliaDHT.Bootstrap(pm.ctx); err != nil {
				pm.logger.Printf("Client: Periodic DHT bootstrap refresh failed: %v", err)
			} else {
				pm.logger.Println("Client: Periodic DHT bootstrap refresh completed/initiated.")
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
	semaphore := make(chan struct{}, 5)

	for _, pi := range pm.bootstrapAddrInfos {
		if pm.host.Network().Connectedness(pi.ID) == network.Connected {
			continue
		}

		wg.Add(1)
		go func(peerInfo peer.AddrInfo) {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}

			connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			if err := pm.host.Connect(connectCtx, peerInfo); err != nil {
				if ctx.Err() == nil {
					pm.logger.Printf("Client: Failed to connect to bootstrap peer %s: %v", peerInfo.ID, err)
				}
			} else {
				pm.logger.Printf("Client: Successfully connected to bootstrap peer: %s", peerInfo.ID.String())
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
		pm.logger.Println("Client: Bootstrap connection attempts phase completed.")
	case <-time.After(2 * time.Minute):
		pm.logger.Println("Client: Bootstrap connection attempts overall timeout reached.")
	case <-ctx.Done():
		pm.logger.Println("Client: Bootstrap connection attempts cancelled by context.")
		return
	}
}

func (pm *P2PManager) serverConnectionManager() {
	defer pm.shutdownWg.Done()
	defer pm.logger.Println("Client: Server connection manager stopped")

	if pm.serverPeerIDStr == "" {
		pm.logger.Println("Client: No server PeerID configured. Server connection manager will not run.")
		return
	}

	targetPeerID, err := peer.Decode(pm.serverPeerIDStr)
	if err != nil {
		pm.logger.Printf("Client: Invalid server PeerID '%s': %v. Server connection manager will not run.", pm.serverPeerIDStr, err)
		return
	}

	ticker := time.NewTicker(pm.healthCheckInterval)
	defer ticker.Stop()

	initialCheckTimer := time.NewTimer(15 * time.Second)
	defer initialCheckTimer.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-initialCheckTimer.C:
			if pm.shouldAttemptConnection(targetPeerID) {
				pm.findAndConnectToServer(pm.ctx, targetPeerID)
			}
		case <-ticker.C:
			if pm.shouldAttemptConnection(targetPeerID) {
				pm.findAndConnectToServer(pm.ctx, targetPeerID)
			}
		}
	}
}

func (pm *P2PManager) shouldAttemptConnection(targetPeerID peer.ID) bool {
	if pm.host.Network().Connectedness(targetPeerID) == network.Connected {
		if time.Since(pm.lastServerContact) < pm.healthCheckInterval {
			return false
		}
		pm.logger.Printf("Client: Connected to server %s, but last contact was > %v ago. Will try to re-verify.", targetPeerID, pm.healthCheckInterval)
	}

	_, _, _, consecutiveFailures, _, lastFailureTime := pm.connectionStats.GetStats()
	if consecutiveFailures > 0 {
		// If there were recent failures, wait for the fixed delay period (2 minutes)
		// since the last failure before attempting again.
		if time.Since(lastFailureTime) < pm.backoffManager.GetDelay() {
			// Optional: Log that we are waiting. Can be noisy.
			// pm.logger.Printf("Client: Waiting for retry delay. %v remaining before next attempt to %s.",
			//	pm.backoffManager.GetDelay()-time.Since(lastFailureTime), targetPeerID)
			return false
		}
	}
	return true
}

func (pm *P2PManager) findAndConnectToServer(ctx context.Context, targetPeerID peer.ID) {
	currentState := ConnectionState(pm.connectionState.Load())
	if currentState == StateConnecting && ctx.Err() == nil {
		pm.logger.Println("Client: Connection attempt already in progress.")
		return
	}
	pm.connectionState.Store(int32(StateConnecting))
	pm.logger.Printf("Client: Attempting to find and connect to server %s...", targetPeerID)

	if addrs := pm.host.Peerstore().Addrs(targetPeerID); len(addrs) > 0 {
		pm.logger.Printf("Client: Server %s has addresses in peerstore: %v. Attempting direct connect.", targetPeerID, addrs)
		connectCtx, cancelDirect := context.WithTimeout(ctx, 15*time.Second)
		if err := pm.host.Connect(connectCtx, peer.AddrInfo{ID: targetPeerID, Addrs: addrs}); err == nil {
			cancelDirect()
			pm.onConnectionSuccess(targetPeerID)
			return
		}
		cancelDirect()
		pm.logger.Printf("Client: Direct connection to %s using cached addrs failed. Trying other methods.", targetPeerID)
	} else {
		pm.logger.Printf("Client: No cached addresses for server %s in peerstore.", targetPeerID)
	}

	if pm.routingDiscovery != nil {
		pm.logger.Printf("Client: Attempting to find server %s via rendezvous...", targetPeerID)
		rendezvousCtx, cancelRdz := context.WithTimeout(ctx, 30*time.Second)
		if pm.findServerViaRendezvous(rendezvousCtx, targetPeerID) {
			cancelRdz()
			if pm.host.Network().Connectedness(targetPeerID) == network.Connected {
				pm.onConnectionSuccess(targetPeerID)
				return
			}
			pm.logger.Printf("Client: Rendezvous found server %s, but not connected. Will proceed to DHT FindPeer.", targetPeerID)
		} else {
			cancelRdz()
			pm.logger.Printf("Client: Server %s not found or connected via rendezvous.", targetPeerID)
		}
	} else {
		pm.logger.Println("Client: RoutingDiscovery not available, skipping rendezvous.")
	}

	pm.logger.Printf("Client: Attempting DHT FindPeer for server %s...", targetPeerID)
	findCtx, findCancel := context.WithTimeout(ctx, 45*time.Second)
	peerInfo, err := pm.kademliaDHT.FindPeer(findCtx, targetPeerID)
	findCancel()

	if err != nil {
		pm.onConnectionFailure(targetPeerID, fmt.Errorf("all discovery methods failed; DHT FindPeer for %s: %w", targetPeerID, err))
		return
	}
	pm.logger.Printf("Client: Found server %s via DHT at addrs: %v", targetPeerID, peerInfo.Addrs)

	if len(peerInfo.Addrs) == 0 {
		pm.onConnectionFailure(targetPeerID, fmt.Errorf("DHT FindPeer for %s succeeded but returned no addresses", targetPeerID))
		return
	}
	pm.host.Peerstore().AddAddrs(peerInfo.ID, peerInfo.Addrs, peerstoreTTL)

	pm.logger.Printf("Client: Attempting to connect to server %s using addresses from DHT.", targetPeerID)
	connectCtx, connectCancel := context.WithTimeout(ctx, 30*time.Second)
	defer connectCancel()
	if err := pm.host.Connect(connectCtx, peerInfo); err != nil {
		pm.onConnectionFailure(targetPeerID, fmt.Errorf("dial to %s (addrs from DHT) failed: %w", targetPeerID, err))
		return
	}

	pm.onConnectionSuccess(targetPeerID)
}

func (pm *P2PManager) onConnectionSuccess(peerID peer.ID) {
	pm.connectionState.Store(int32(StateConnected))
	pm.connectionStats.RecordAttempt(true) // This resets consecutiveFailures
	// pm.backoffManager.Reset() // No longer needed

	pm.lastServerContact = time.Now()
	pm.logger.Printf("Client: Successfully connected to server %s", peerID)
}

func (pm *P2PManager) onConnectionFailure(peerID peer.ID, err error) {
	pm.connectionState.Store(int32(StateFailed))
	// Get the fixed retry delay for logging purposes.
	// The actual waiting happens due to the ticker in serverConnectionManager
	// and the shouldAttemptConnection logic.
	retryDelay := pm.backoffManager.GetDelay()
	pm.connectionStats.RecordAttempt(false) // This increments consecutiveFailures and updates lastFailureTime

	if pm.ctx.Err() == nil {
		pm.logger.Printf("Client: Failed to connect/find server %s: %v (will retry after ~%s if still disconnected)", peerID, err, retryDelay)
	}
}

func (pm *P2PManager) addressMonitor() {
	defer pm.shutdownWg.Done()
	defer pm.logger.Println("Client: Address monitor stopped")

	addrSub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated))
	if err != nil {
		pm.logger.Printf("Client: Failed to subscribe to address updates: %v", err)
		return
	}
	defer addrSub.Close()

	reachabilitySub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		pm.logger.Printf("Client: Failed to subscribe to reachability events: %v", err)
		reachabilitySub = nil
	} else {
		defer reachabilitySub.Close()
	}

	pm.logger.Printf("Client: Initial listen addresses: %v", pm.host.Addrs())

	var addrChan <-chan interface{}
	var reachabilityChan <-chan interface{}

	if addrSub != nil {
		addrChan = addrSub.Out()
	}
	if reachabilitySub != nil {
		reachabilityChan = reachabilitySub.Out()
	}

	for {
		select {
		case <-pm.ctx.Done():
			return
		case ev, ok := <-addrChan:
			if !ok {
				pm.logger.Println("Client: Address subscription closed")
				addrChan = nil
				if reachabilityChan == nil {
					return
				}
				continue
			}
			addressEvent := ev.(event.EvtLocalAddressesUpdated)
			pm.logger.Printf("Client: Addresses updated. Current: %v", len(addressEvent.Current))
			for _, addr := range addressEvent.Current {
				pm.logger.Printf("Client: Current address: %v (Action: %v)", addr.Address, addr.Action)
			}
			if addressEvent.Diffs && len(addressEvent.Removed) > 0 {
				pm.logger.Println("Client: Removed addresses:")
				for _, addr := range addressEvent.Removed {
					pm.logger.Printf("Client: Removed address: %v", addr.Address)
				}
			}
		case ev, ok := <-reachabilityChan:
			if !ok {
				pm.logger.Println("Client: Reachability subscription closed")
				reachabilityChan = nil
				if addrChan == nil {
					return
				}
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

func (pm *P2PManager) SendLogBatch(ctx context.Context, clientAppID string, encryptedPayload []byte) (*p2p.LogBatchResponse, error) {
	select {
	case pm.rateLimiter <- struct{}{}:
		defer func() { <-pm.rateLimiter }()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("rate limit exceeded: too many concurrent SendLogBatch requests")
	}

	if pm.serverPeerIDStr == "" {
		return nil, fmt.Errorf("server PeerID not configured")
	}
	targetPeerID, err := peer.Decode(pm.serverPeerIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid server PeerID '%s': %w", pm.serverPeerIDStr, err)
	}

	if pm.host.Network().Connectedness(targetPeerID) != network.Connected {
		pm.logger.Printf("Client: Not connected to server %s. SendLogBatch will fail. Connection manager should handle reconnection.", targetPeerID)
		return nil, fmt.Errorf("not connected to server %s", targetPeerID)
	}

	streamCtx, cancelStreamOpen := context.WithTimeout(ctx, 30*time.Second)
	stream, err := pm.host.NewStream(streamCtx, targetPeerID, p2p.ProtocolID)
	cancelStreamOpen()

	if err != nil {
		return nil, fmt.Errorf("failed to open new stream to server %s: %w", targetPeerID, err)
	}
	pm.logger.Printf("Client: Created new stream to server %s for LogBatch", targetPeerID)
	defer stream.Reset()

	req := p2p.LogBatchRequest{
		Header:              p2p.NewMessageHeader(p2p.MessageTypeLogBatch),
		AppClientID:         clientAppID,
		EncryptedLogPayload: encryptedPayload,
	}

	if err := stream.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, fmt.Errorf("failed to set write deadline for LogBatchRequest: %w", err)
	}
	if err := p2p.WriteMessage(stream, &req); err != nil {
		return nil, fmt.Errorf("failed to write LogBatchRequest: %w", err)
	}

	if err := stream.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
		return nil, fmt.Errorf("failed to set read deadline for LogBatchResponse: %w", err)
	}
	var resp p2p.LogBatchResponse
	if err := p2p.ReadMessage(stream, &resp); err != nil {
		return nil, fmt.Errorf("failed to read LogBatchResponse: %w", err)
	}

	if resp.Header.MessageType != p2p.MessageTypeLogBatch && resp.Header.MessageType != p2p.MessageTypeError {
		return nil, fmt.Errorf("unexpected response message type: %s (RequestID: %s)", resp.Header.MessageType, req.Header.RequestID)
	}

	if resp.Header.RequestID != req.Header.RequestID {
		return nil, fmt.Errorf("response RequestID mismatch: expected %s, got %s",
			req.Header.RequestID, resp.Header.RequestID)
	}

	pm.lastServerContact = time.Now()
	pm.logger.Printf("Client: Successfully sent batch and received response from server %s. Updated last contact time.", targetPeerID)

	return &resp, nil
}

func (pm *P2PManager) GetConnectionStats() (ConnectionState, *ConnectionStats) {
	state := ConnectionState(pm.connectionState.Load())
	return state, pm.connectionStats
}

func (pm *P2PManager) Close() error {
	pm.logger.Println("Client: Shutting down P2P Manager...")
	pm.cancel()

	done := make(chan struct{})
	go func() {
		pm.shutdownWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		pm.logger.Println("Client: All P2P background goroutines stopped.")
	case <-time.After(10 * time.Second):
		pm.logger.Println("Client: Timeout waiting for P2P background goroutines to stop.")
	}

	pm.streamPool.Cleanup()

	if pm.kademliaDHT != nil {
		pm.logger.Println("Client: Closing Kademlia DHT...")
		if err := pm.kademliaDHT.Close(); err != nil {
			pm.logger.Printf("Client: Error closing Kademlia DHT: %v", err)
		}
	}

	if pm.host != nil {
		pm.logger.Println("Client: Closing libp2p host...")
		if err := pm.host.Close(); err != nil {
			pm.logger.Printf("Client: Error closing libp2p host: %v", err)
			return err
		}
	}
	pm.logger.Println("Client: P2P Manager shut down complete.")
	return nil
}

func runP2PManager(p2pManager *P2PManager, internalQuit <-chan struct{}, wg *sync.WaitGroup, logger *log.Logger) {
	defer wg.Done()
	p2pManager.logger.Println("Client P2P Manager controlling goroutine starting.")
	p2pManager.StartDiscoveryAndBootstrap(p2pManager.ctx)

	select {
	case <-internalQuit:
		p2pManager.logger.Println("Client P2P Manager controlling goroutine: Received internal quit signal.")
	case <-p2pManager.ctx.Done():
		p2pManager.logger.Println("Client P2P Manager controlling goroutine: P2PManager internal context done.")
	}
	p2pManager.logger.Println("Client P2P Manager controlling goroutine finished.")
}
