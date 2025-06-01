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
			// Bootstrap peers are now primarily for the main host. DHT will use them if connected.
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
		streamPool:          NewStreamPool(), // Initialize even if not used by SendLogBatch
		ctx:                 ctx,
		cancel:              cancel,
		healthCheckInterval: 30 * time.Second,
		rateLimiter:         make(chan struct{}, 10), // Max 10 concurrent SendLogBatch calls
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

					// Attempt to connect immediately if addresses found
					connectCtx, connectCancel := context.WithTimeout(ctx, 15*time.Second)
					if err := pm.host.Connect(connectCtx, pi); err == nil {
						pm.logger.Printf("Client: Successfully connected to server %s found via rendezvous.", targetPeerID)
						foundTarget = true
					} else {
						pm.logger.Printf("Client: Failed to connect to server %s found via rendezvous: %v", targetPeerID, err)
					}
					connectCancel()
					// If connected, we can stop searching
					if foundTarget {
						return true
					}
				} else {
					pm.logger.Printf("Client: Target server %s found via rendezvous but no addresses advertised with it.", targetPeerID)
				}
				// Even if connection failed or no addrs, we found it. If other methods fail, DHT might resolve it later.
				// For this function's purpose, we can consider it found conceptually.
				// Set foundTarget true if we want to prioritize this method even if immediate connect fails.
				// For now, only true if connect succeeds.
			}
		}
	}
}
func (pm *P2PManager) StartDiscoveryAndBootstrap(ctx context.Context) {
	pm.logger.Println("Client: Starting discovery and bootstrap...")

	if len(pm.bootstrapAddrInfos) == 0 {
		pm.logger.Println("Client: No bootstrap peers; DHT will be limited")
	}

	// Initial attempt to connect to any bootstrap peers
	// This makes the DHT bootstrap more effective if connections are live.
	pm.connectToBootstrapPeers(ctx)

	// Now start the managers
	pm.shutdownWg.Add(4) // bootstrapManager, serverConnectionManager, addressMonitor, cleanupManager
	go pm.bootstrapManager()
	go pm.serverConnectionManager()
	go pm.addressMonitor()
	go pm.cleanupManager()
}

func (pm *P2PManager) bootstrapManager() {
	defer pm.shutdownWg.Done()
	defer pm.logger.Println("Client: Bootstrap manager stopped")

	// Initial delay before first DHT bootstrap, allows host to settle and connect to some peers.
	initialTimer := time.NewTimer(30 * time.Second)
	defer initialTimer.Stop()

	// Ticker for retrying connections to bootstrap peers if they drop.
	retryTicker := time.NewTicker(3 * time.Minute)
	defer retryTicker.Stop()

	// Ticker for periodic DHT refresh/bootstrap.
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
					// Initialize RoutingDiscovery after first DHT bootstrap attempt
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
	// Limit concurrent dials to avoid overwhelming the system or network.
	semaphore := make(chan struct{}, 5) // Max 5 concurrent dials

	for _, pi := range pm.bootstrapAddrInfos {
		// Skip if already connected to this peer
		if pm.host.Network().Connectedness(pi.ID) == network.Connected {
			continue
		}

		wg.Add(1)
		go func(peerInfo peer.AddrInfo) {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done(): // Check if context was cancelled before starting
				return
			}

			connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second) // Timeout for this specific connection attempt
			defer cancel()

			if err := pm.host.Connect(connectCtx, peerInfo); err != nil {
				if ctx.Err() == nil { // Log only if the main context isn't done
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
	case <-time.After(2 * time.Minute): // Overall timeout for all bootstrap connections
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

	// Ticker for periodic health checks/connection attempts.
	ticker := time.NewTicker(pm.healthCheckInterval)
	defer ticker.Stop()

	// Perform an initial connection attempt fairly quickly after startup.
	initialCheckTimer := time.NewTimer(15 * time.Second) // Give some time for DHT bootstrap to kick in
	defer initialCheckTimer.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-initialCheckTimer.C: // Use up the timer for the first check
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
	// If already connected and last contact was recent (within health check interval), likely okay.
	if pm.host.Network().Connectedness(targetPeerID) == network.Connected {
		if time.Since(pm.lastServerContact) < pm.healthCheckInterval { // More aggressive check
			return false
		}
		pm.logger.Printf("Client: Connected to server %s, but last contact was > %v ago. Will try to re-verify.", targetPeerID, pm.healthCheckInterval)
	}

	// Check backoff: if we failed recently, wait for the backoff period.
	_, _, _, consecutiveFailures, _, lastFailureTime := pm.connectionStats.GetStats()
	if consecutiveFailures > 0 {
		// Important: Use backoffManager.attempts, not consecutiveFailures for calculating delay
		// as Failure() increments attempts. We need the *next* delay based on current attempts.
		// To get current delay for current attempts, we'd calculate based on `atomic.LoadInt64(&bm.attempts)`.
		// For now, let's assume lastFailureTime implies a backoff was set *after* that failure.
		// The backoffManager.Failure() call in onConnectionFailure calculates the *next* delay.
		// Here, we are checking if enough time has passed since the *last failure*.
		// A better way: store the *next retry time* instead of just last failure.
		// For simplicity now, use a fixed delay for retrying after a failure if backoff is active.
		// Let's use the BaseDelay as a minimum wait after any failure.
		// A proper backoff strategy would involve `pm.backoffManager.NextDelay()` (if it existed)
		// or calculating it based on current `pm.backoffManager.attempts`.

		// Simplified: if there was a failure, wait at least baseDelay before retrying.
		// The `pm.backoffManager.Failure()` in `onConnectionFailure` actually calculates the *next* backoff.
		// If time since lastFailure < calculated backoff time for that failure, then don't retry.
		// This is complex to get right without storing the calculated backoff time.
		// Let's assume onConnectionFailure sets the state to StateFailed, and we retry on ticker.
		// The backoffManager.Failure() in onConnectionFailure effectively logs the *next* delay.
		// This check is more about "should we even try now or is it too soon after a failure".
		// If the time since last failure is less than the base delay, probably too soon.
		if time.Since(lastFailureTime) < pm.backoffManager.baseDelay*time.Duration(math.Min(float64(consecutiveFailures), 5)) { // Basic linear backoff for check
			return false
		}
	}
	return true
}

func (pm *P2PManager) findAndConnectToServer(ctx context.Context, targetPeerID peer.ID) {
	currentState := ConnectionState(pm.connectionState.Load())
	if currentState == StateConnecting && ctx.Err() == nil { // Avoid re-entry if not cancelled
		pm.logger.Println("Client: Connection attempt already in progress.")
		return
	}
	pm.connectionState.Store(int32(StateConnecting))
	pm.logger.Printf("Client: Attempting to find and connect to server %s...", targetPeerID)

	// 1. Try direct connection if addresses are already in peerstore (cached from previous success or bootstrap).
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

	// 2. Try rendezvous discovery (if routingDiscovery is initialized).
	if pm.routingDiscovery != nil {
		pm.logger.Printf("Client: Attempting to find server %s via rendezvous...", targetPeerID)
		rendezvousCtx, cancelRdz := context.WithTimeout(ctx, 30*time.Second) // Timeout for rendezvous attempt
		if pm.findServerViaRendezvous(rendezvousCtx, targetPeerID) {
			// findServerViaRendezvous itself attempts connection and calls onConnectionSuccess if it works
			// So, if it returns true, we assume connection was successful or addresses were added.
			// Check connectedness again.
			cancelRdz()
			if pm.host.Network().Connectedness(targetPeerID) == network.Connected {
				pm.onConnectionSuccess(targetPeerID) // Ensure state is updated if findServerViaRendezvous connected
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

	// 3. Fall back to DHT FindPeer.
	pm.logger.Printf("Client: Attempting DHT FindPeer for server %s...", targetPeerID)
	findCtx, findCancel := context.WithTimeout(ctx, 45*time.Second) // Timeout for DHT FindPeer
	peerInfo, err := pm.kademliaDHT.FindPeer(findCtx, targetPeerID)
	findCancel() // Release resources for findCtx

	if err != nil {
		pm.onConnectionFailure(targetPeerID, fmt.Errorf("all discovery methods failed; DHT FindPeer for %s: %w", targetPeerID, err))
		return
	}
	pm.logger.Printf("Client: Found server %s via DHT at addrs: %v", targetPeerID, peerInfo.Addrs)

	if len(peerInfo.Addrs) == 0 {
		pm.onConnectionFailure(targetPeerID, fmt.Errorf("DHT FindPeer for %s succeeded but returned no addresses", targetPeerID))
		return
	}
	pm.host.Peerstore().AddAddrs(peerInfo.ID, peerInfo.Addrs, peerstoreTTL) // Cache addresses

	pm.logger.Printf("Client: Attempting to connect to server %s using addresses from DHT.", targetPeerID)
	connectCtx, connectCancel := context.WithTimeout(ctx, 30*time.Second) // Timeout for connection attempt
	defer connectCancel()
	if err := pm.host.Connect(connectCtx, peerInfo); err != nil {
		pm.onConnectionFailure(targetPeerID, fmt.Errorf("dial to %s (addrs from DHT) failed: %w", targetPeerID, err))
		return
	}

	pm.onConnectionSuccess(targetPeerID)
}

func (pm *P2PManager) onConnectionSuccess(peerID peer.ID) {
	pm.connectionState.Store(int32(StateConnected))
	pm.connectionStats.RecordAttempt(true)
	pm.backoffManager.Reset() // Reset backoff on successful connection

	pm.lastServerContact = time.Now() // Update last contact time
	pm.logger.Printf("Client: Successfully connected to server %s", peerID)
}

func (pm *P2PManager) onConnectionFailure(peerID peer.ID, err error) {
	pm.connectionState.Store(int32(StateFailed))
	// Calculate and log the backoff for the *next* attempt.
	// The actual waiting happens due to the ticker in serverConnectionManager
	// and the shouldAttemptConnection logic.
	nextBackoffDuration := pm.backoffManager.Failure()
	pm.connectionStats.RecordAttempt(false)

	// Only log if the context isn't already cancelled (i.e., not shutting down)
	if pm.ctx.Err() == nil {
		pm.logger.Printf("Client: Failed to connect/find server %s: %v (next retry attempt after ~%s)", peerID, err, nextBackoffDuration)
	}
}

func (pm *P2PManager) addressMonitor() {
	defer pm.shutdownWg.Done()
	defer pm.logger.Println("Client: Address monitor stopped")

	// Subscribe to address update events.
	addrSub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated))
	if err != nil {
		pm.logger.Printf("Client: Failed to subscribe to address updates: %v", err)
		return
	}
	defer addrSub.Close()

	// Subscribe to reachability change events.
	reachabilitySub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		pm.logger.Printf("Client: Failed to subscribe to reachability events: %v", err)
		// Continue without reachability events if subscription fails
		reachabilitySub = nil
	} else {
		defer reachabilitySub.Close()
	}

	pm.logger.Printf("Client: Initial listen addresses: %v", pm.host.Addrs())

	// Get the channels once to avoid repeated function calls
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
				addrChan = nil // Set to nil to disable this case
				// If both channels are nil, exit
				if reachabilityChan == nil {
					return
				}
				continue
			}

			addressEvent := ev.(event.EvtLocalAddressesUpdated)
			pm.logger.Printf("Client: Addresses updated. Current: %v", len(addressEvent.Current))

			// Log current addresses
			for _, addr := range addressEvent.Current {
				pm.logger.Printf("Client: Current address: %v (Action: %v)", addr.Address, addr.Action)
			}

			// Log removed addresses if diffs are available
			if addressEvent.Diffs && len(addressEvent.Removed) > 0 {
				pm.logger.Println("Client: Removed addresses:")
				for _, addr := range addressEvent.Removed {
					pm.logger.Printf("Client: Removed address: %v", addr.Address)
				}
			}

		case ev, ok := <-reachabilityChan:
			if !ok {
				pm.logger.Println("Client: Reachability subscription closed")
				reachabilityChan = nil // Set to nil to disable this case
				// If both channels are nil, exit
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

	ticker := time.NewTicker(5 * time.Minute) // Periodically clean up stream pool
	defer ticker.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-ticker.C:
			pm.streamPool.Cleanup() // Clean up any old/closed streams from the general pool
		}
	}
}

// SendLogBatch sends client’s log batch to server.
// It will now ALWAYS create a new stream and not use the pool for this operation
// because the server closes the stream after each request.
func (pm *P2PManager) SendLogBatch(ctx context.Context, clientAppID string, encryptedPayload []byte) (*p2p.LogBatchResponse, error) {
	// Rate limiting
	select {
	case pm.rateLimiter <- struct{}{}:
		defer func() { <-pm.rateLimiter }()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Second): // Timeout for acquiring a rate limit slot
		return nil, fmt.Errorf("rate limit exceeded: too many concurrent SendLogBatch requests")
	}

	if pm.serverPeerIDStr == "" {
		return nil, fmt.Errorf("server PeerID not configured")
	}
	targetPeerID, err := peer.Decode(pm.serverPeerIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid server PeerID '%s': %w", pm.serverPeerIDStr, err)
	}

	// Check if connected to the server.
	if pm.host.Network().Connectedness(targetPeerID) != network.Connected {
		pm.logger.Printf("Client: Not connected to server %s. SendLogBatch will fail. Connection manager should handle reconnection.", targetPeerID)
		return nil, fmt.Errorf("not connected to server %s", targetPeerID)
	}

	// ALWAYS create a new stream for this request-response protocol.
	streamCtx, cancelStreamOpen := context.WithTimeout(ctx, 30*time.Second) // Timeout for opening the stream
	stream, err := pm.host.NewStream(streamCtx, targetPeerID, p2p.ProtocolID)
	cancelStreamOpen() // Release resources associated with streamCtx once NewStream returns

	if err != nil {
		// No need to use pm.streamPool.Remove as we are not using the pool here.
		return nil, fmt.Errorf("failed to open new stream to server %s: %w", targetPeerID, err)
	}
	pm.logger.Printf("Client: Created new stream to server %s for LogBatch", targetPeerID)

	// Ensure stream is reset (more forceful than close) when done with this specific operation.
	defer stream.Reset()

	// Build and send request
	req := p2p.LogBatchRequest{
		Header:              p2p.NewMessageHeader(p2p.MessageTypeLogBatch),
		AppClientID:         clientAppID,
		EncryptedLogPayload: encryptedPayload,
	}

	// Set deadline for writing the request
	if err := stream.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, fmt.Errorf("failed to set write deadline for LogBatchRequest: %w", err)
	}
	if err := p2p.WriteMessage(stream, &req); err != nil {
		return nil, fmt.Errorf("failed to write LogBatchRequest: %w", err)
	}

	// Set deadline for reading the response
	if err := stream.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
		return nil, fmt.Errorf("failed to set read deadline for LogBatchResponse: %w", err)
	}
	var resp p2p.LogBatchResponse
	if err := p2p.ReadMessage(stream, &resp); err != nil {
		return nil, fmt.Errorf("failed to read LogBatchResponse: %w", err)
	}

	// Validate message type
	if resp.Header.MessageType != p2p.MessageTypeLogBatch && resp.Header.MessageType != p2p.MessageTypeError {
		return nil, fmt.Errorf("unexpected response message type: %s (RequestID: %s)", resp.Header.MessageType, req.Header.RequestID)
	}

	// Validate RequestID matches
	if resp.Header.RequestID != req.Header.RequestID {
		return nil, fmt.Errorf("response RequestID mismatch: expected %s, got %s",
			req.Header.RequestID, resp.Header.RequestID)
	}

	// If the send was successful and we got a valid response, update last contact time.
	pm.lastServerContact = time.Now()
	pm.logger.Printf("Client: Successfully sent batch and received response from server %s. Updated last contact time.", targetPeerID)

	return &resp, nil
}

// GetConnectionStats returns the current ConnectionState and stats object.
func (pm *P2PManager) GetConnectionStats() (ConnectionState, *ConnectionStats) {
	state := ConnectionState(pm.connectionState.Load())
	// Create a copy of stats to avoid race conditions if the caller modifies it,
	// though RWMutex in ConnectionStats mostly protects its internal fields.
	// For simplicity, direct return is fine if caller treats it as read-only.
	return state, pm.connectionStats
}

// Close gracefully shuts down background goroutines, DHT, and host.
func (pm *P2PManager) Close() error {
	pm.logger.Println("Client: Shutting down P2P Manager...")

	// Cancel context to signal all managed goroutines to stop.
	pm.cancel()

	// Wait for background goroutines to finish.
	done := make(chan struct{})
	go func() {
		pm.shutdownWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		pm.logger.Println("Client: All P2P background goroutines stopped.")
	case <-time.After(10 * time.Second): // Timeout for waiting
		pm.logger.Println("Client: Timeout waiting for P2P background goroutines to stop.")
	}

	// Clean up the general purpose stream pool.
	pm.streamPool.Cleanup()

	// Close Kademlia DHT.
	if pm.kademliaDHT != nil {
		pm.logger.Println("Client: Closing Kademlia DHT...")
		if err := pm.kademliaDHT.Close(); err != nil {
			pm.logger.Printf("Client: Error closing Kademlia DHT: %v", err)
		}
	}

	// Close the libp2p host.
	if pm.host != nil {
		pm.logger.Println("Client: Closing libp2p host...")
		if err := pm.host.Close(); err != nil {
			pm.logger.Printf("Client: Error closing libp2p host: %v", err)
			return err // Return the host close error if any
		}
	}
	pm.logger.Println("Client: P2P Manager shut down complete.")
	return nil
}

// runP2PManager is the entry point for running the P2PManager in its own goroutine,
// managed by the main application's WaitGroup and quit channel.
func runP2PManager(p2pManager *P2PManager, internalQuit <-chan struct{}, wg *sync.WaitGroup, logger *log.Logger) {
	defer wg.Done()
	p2pManager.logger.Println("Client P2P Manager controlling goroutine starting.")

	// The P2PManager's internal context (p2pManager.ctx) is used for its own goroutines.
	// This outer goroutine primarily starts the P2PManager's operations and waits for shutdown.
	p2pManager.StartDiscoveryAndBootstrap(p2pManager.ctx) // Start its internal goroutines

	// Wait for the main application's quit signal
	select {
	case <-internalQuit:
		p2pManager.logger.Println("Client P2P Manager controlling goroutine: Received internal quit signal.")
		// The P2PManager's Close method (which calls its internal p2pManager.cancel())
		// will be called when the main application defer executes.
		// No explicit call to p2pManager.Close() here, as it's handled by defer in main.
	case <-p2pManager.ctx.Done(): // If P2PManager's internal context is cancelled for other reasons
		p2pManager.logger.Println("Client P2P Manager controlling goroutine: P2PManager internal context done.")
	}
	p2pManager.logger.Println("Client P2P Manager controlling goroutine finished.")
}
