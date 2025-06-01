// gui_generator_app/generator/templates/client_template/p2p_manager.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	// For net.Error
	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/pkg/p2p"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	coreRouting "github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	routd "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
)

const ServerRendezvousString = "guikey-standalone-logserver/v1.0.0"
const ClientLocalDiscoveryServiceTag = "guikey-standalone-logclient-local" // Differentiate from server if client also advertises

const (
	peerstoreTTL = 2 * time.Hour
)

type ConnectionState int32

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateFailed
)

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
	return cs.totalAttempts, cs.successfulAttempts, cs.failedAttempts, cs.consecutiveFailures, cs.lastSuccess, cs.lastFailure
}

type BackoffManager struct{ fixedDelay time.Duration }

func NewBackoffManager() *BackoffManager           { return &BackoffManager{fixedDelay: 2 * time.Minute} }
func (bm *BackoffManager) GetDelay() time.Duration { return bm.fixedDelay }

// StreamPool (kept for example, but not used by SendLogBatch currently)
type StreamPool struct {
	streams map[peer.ID]*pooledStream
	maxAge  time.Duration
}
type pooledStream struct {
	// ...
}

func NewStreamPool() *StreamPool {
	return &StreamPool{
		streams: make(map[peer.ID]*pooledStream),
		maxAge:  10 * time.Minute,
	}
}
func (sp *StreamPool) Get(peerID peer.ID) network.Stream         { /* ... */ return nil }
func (sp *StreamPool) Put(peerID peer.ID, stream network.Stream) { /* ... */ }
func (sp *StreamPool) Remove(peerID peer.ID)                     { /* ... */ }
func (sp *StreamPool) Cleanup()                                  { /* ... */ }

type P2PManager struct {
	host               host.Host
	kademliaDHT        *dht.IpfsDHT
	routingDiscovery   *routd.RoutingDiscovery
	mdnsService        mdns.Service // For mDNS local discovery
	logger             *log.Logger
	serverPeerIDStr    string
	bootstrapAddrInfos []peer.AddrInfo

	connectionState atomic.Int32
	connectionStats *ConnectionStats
	backoffManager  *BackoffManager
	streamPool      *StreamPool // Not used for SendLogBatch

	ctx        context.Context    // Main context for P2P operations
	cancel     context.CancelFunc // To cancel P2P operations
	shutdownWg sync.WaitGroup     // For graceful shutdown of goroutines

	lastServerContact   time.Time
	healthCheckInterval time.Duration
	rateLimiter         chan struct{}
}

// clientMdnsNotifee implements discovery.Notifee for mDNS on the client side
type clientMdnsNotifee struct {
	logger          *log.Logger
	h               host.Host
	targetPeerIDStr string      // Store the target server's PeerID string
	p2pManager      *P2PManager // To access peerstore and potentially trigger connections
}

func (m *clientMdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	m.logger.Printf("Client P2P (mDNS): Discovered local peer %s with addrs: %v", pi.ID, pi.Addrs)
	targetPID, err := peer.Decode(m.targetPeerIDStr)
	if err != nil {
		m.logger.Printf("Client P2P (mDNS) ERROR: Could not decode target peer ID '%s' for comparison: %v", m.targetPeerIDStr, err)
		return
	}

	if pi.ID == targetPID {
		m.logger.Printf("Client P2P (mDNS): Found target server %s via mDNS!", pi.ID)
		if len(pi.Addrs) > 0 {
			// Use our own TTL instead of mdns.DefaultTTL (removed)
			m.h.Peerstore().AddAddrs(pi.ID, pi.Addrs, peerstoreTTL)
			m.logger.Printf("Client P2P (mDNS): Added/updated addresses for target server %s in peerstore from mDNS.", pi.ID)
			// Potentially trigger a connection attempt if not already connected or trying.
			if m.p2pManager.shouldAttemptConnection(targetPID) {
				m.logger.Printf("Client P2P (mDNS): Triggering findAndConnectToServer for %s based on mDNS discovery.", targetPID)
				go m.p2pManager.findAndConnectToServer(m.p2pManager.ctx, targetPID) // Run in goroutine to not block notifee
			}
		}
	}
}

func NewP2PManager(logger *log.Logger, serverPeerID string, bootstrapAddrs []string) (*P2PManager, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "P2P_CLIENT_FALLBACK: ", log.LstdFlags|log.Lshortfile)
	}

	var bootAddrsInfo []peer.AddrInfo
	for _, addrStr := range bootstrapAddrs {
		if addrStr == "" {
			continue
		}
		ai, err := peer.AddrInfoFromString(addrStr)
		if err != nil {
			logger.Printf("Client P2P WARN: Invalid bootstrap address '%s': %v", addrStr, err)
			continue
		}
		bootAddrsInfo = append(bootAddrsInfo, *ai)
	}
	if len(bootAddrsInfo) == 0 {
		logger.Println("Client P2P WARN: No valid bootstrap addresses; DHT/Relay functionality will be limited")
	}

	ctx, cancel := context.WithCancel(context.Background())

	limiter := rcmgr.NewFixedLimiter(rcmgr.DefaultLimits.AutoScale())
	rm, err := rcmgr.NewResourceManager(limiter)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("client_p2p: failed to create resource manager: %w", err)
	}

	var kadDHT *dht.IpfsDHT
	opts := []libp2p.Option{
		libp2p.ListenAddrStrings( // Client also listens for hole punching and potential incoming connections
			"/ip4/0.0.0.0/tcp/0",
			"/ip4/0.0.0.0/udp/0/quic-v1",
			"/ip6/::/tcp/0",
			"/ip6/::/udp/0/quic-v1",
		),
		libp2p.ResourceManager(rm),
		libp2p.Routing(func(h host.Host) (coreRouting.PeerRouting, error) {
			var dhtErr error
			dhtOpts := []dht.Option{dht.Mode(dht.ModeClient), dht.ProtocolPrefix("/p2p")}
			if len(bootAddrsInfo) > 0 {
				dhtOpts = append(dhtOpts, dht.BootstrapPeers(bootAddrsInfo...))
			}
			kadDHT, dhtErr = dht.New(ctx, h, dhtOpts...) // Use manager's context for DHT
			return kadDHT, dhtErr
		}),
		libp2p.EnableHolePunching(),
		libp2p.EnableNATService(), // Client can also try UPnP if it becomes a listener for some reason
		libp2p.NATPortMap(),
		// Client should actively seek relays if direct connection fails
		libp2p.EnableRelay(), // Enables client to use relays, does not mean it becomes a relay service
	}

	if len(bootAddrsInfo) > 0 {
		// Use the autorelay package for `WithStaticRelays`, `WithNumRelays`, etc. :contentReference[oaicite:2]{index=2}
		opts = append(opts, libp2p.EnableAutoRelay(
			autorelay.WithStaticRelays(bootAddrsInfo),
			autorelay.WithNumRelays(3),
			autorelay.WithMinCandidates(4),
			autorelay.WithBootDelay(60*time.Second),
		))
	} else {
		opts = append(opts, libp2p.EnableAutoRelay()) // Fallback if no static relays defined
	}

	p2pHost, err := libp2p.New(opts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("client_p2p: failed to create libp2p host: %w", err)
	}
	logger.Printf("Client P2P: Host created with ID: %s. Listening on: %v", p2pHost.ID().String(), p2pHost.Addrs())

	if kadDHT == nil {
		p2pHost.Close()
		cancel()
		return nil, fmt.Errorf("client_p2p: Kademlia DHT was not initialized")
	}

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
		healthCheckInterval: 45 * time.Second,       // Slightly longer health check interval
		rateLimiter:         make(chan struct{}, 5), // Limit concurrent SendLogBatch
	}
	pm.connectionState.Store(int32(StateDisconnected))

	// Setup mDNS for local discovery for the client
	mdnsSvc := mdns.NewMdnsService(
		p2pHost,
		ClientLocalDiscoveryServiceTag,
		&clientMdnsNotifee{logger: logger, h: p2pHost, targetPeerIDStr: serverPeerID, p2pManager: pm},
	) // No error returned – just a Service handle :contentReference[oaicite:3]{index=3}
	pm.mdnsService = mdnsSvc

	// Initialize RoutingDiscovery after DHT is confirmed to be non-nil
	pm.routingDiscovery = routd.NewRoutingDiscovery(kadDHT)
	logger.Println("Client P2P: RoutingDiscovery initialized.")

	return pm, nil
}

func (pm *P2PManager) StartDiscoveryAndBootstrap() { // Removed context argument, uses internal pm.ctx
	pm.logger.Println("Client P2P: Starting discovery and bootstrap processes...")
	// Goroutines managed by P2PManager will use pm.ctx for cancellation.
	pm.shutdownWg.Add(4) // bootstrapManager, serverConnectionManager, addressMonitor, cleanupManager

	go pm.bootstrapManager()
	go pm.serverConnectionManager()
	go pm.addressMonitor()
	go pm.cleanupManager()

	if pm.mdnsService != nil {
		pm.shutdownWg.Add(1)
		go func() {
			defer pm.shutdownWg.Done()
			pm.logger.Println("Client P2P: Starting mDNS local discovery service...")
			if err := pm.mdnsService.Start(); err != nil {
				pm.logger.Printf("Client P2P WARN: Failed to start mDNS service: %v", err)
			} else {
				pm.logger.Println("Client P2P: mDNS local discovery service started.")
				<-pm.ctx.Done() // Keep mDNS running until P2P manager context is cancelled
				pm.logger.Println("Client P2P: mDNS service stopping due to P2P context cancellation.")
			}
		}()
	}
}

func (pm *P2PManager) bootstrapManager() {
	defer pm.shutdownWg.Done()
	defer pm.logger.Println("Client P2P: Bootstrap manager stopped")

	initialConnectTimer := time.NewTimer(5 * time.Second) // Quicker initial connect attempt
	defer initialConnectTimer.Stop()

	initialBootstrapTimer := time.NewTimer(20 * time.Second) // Initial DHT bootstrap after connects
	defer initialBootstrapTimer.Stop()

	retryConnectTicker := time.NewTicker(3 * time.Minute)
	defer retryConnectTicker.Stop()

	dhtRefreshTicker := time.NewTicker(15 * time.Minute)
	defer dhtRefreshTicker.Stop()

	firstBootstrapDone := false

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-initialConnectTimer.C:
			pm.connectToBootstrapPeers(pm.ctx)

		case <-initialBootstrapTimer.C:
			if !firstBootstrapDone && pm.ctx.Err() == nil {
				pm.logger.Println("Client P2P: Performing initial DHT bootstrap...")
				dhtCtx, cancelDht := context.WithTimeout(pm.ctx, 2*time.Minute)
				if err := pm.kademliaDHT.Bootstrap(dhtCtx); err != nil {
					if dhtCtx.Err() == nil {
						pm.logger.Printf("Client P2P: Initial DHT bootstrap failed: %v", err)
					}
				} else {
					pm.logger.Println("Client P2P: Initial DHT bootstrap process completed/initiated.")
				}
				cancelDht()
				firstBootstrapDone = true
			}

		case <-retryConnectTicker.C:
			if pm.ctx.Err() != nil {
				return
			}
			pm.logger.Println("Client P2P: Retrying bootstrap peer connections...")
			pm.connectToBootstrapPeers(pm.ctx)

		case <-dhtRefreshTicker.C:
			if pm.ctx.Err() != nil {
				return
			}
			pm.logger.Println("Client P2P: Performing periodic DHT bootstrap refresh...")
			dhtCtx, cancelDht := context.WithTimeout(pm.ctx, 2*time.Minute)
			if err := pm.kademliaDHT.Bootstrap(dhtCtx); err != nil {
				if dhtCtx.Err() == nil {
					pm.logger.Printf("Client P2P: Periodic DHT bootstrap refresh failed: %v", err)
				}
			} else {
				pm.logger.Println("Client P2P: Periodic DHT bootstrap refresh completed/initiated.")
			}
			cancelDht()
		}
	}
}

func (pm *P2PManager) connectToBootstrapPeers(ctx context.Context) {
	if len(pm.bootstrapAddrInfos) == 0 {
		return
	}
	pm.logger.Printf("Client P2P: Attempting to connect to %d bootstrap peers...", len(pm.bootstrapAddrInfos))

	var wg sync.WaitGroup
	// Use a semaphore to limit concurrent connection attempts
	semaphore := make(chan struct{}, 5) // Limit to 5 concurrent bootstrap attempts

	connectCtx, cancelAllConnects := context.WithTimeout(ctx, 90*time.Second) // Overall timeout for this phase
	defer cancelAllConnects()

	for _, pi := range pm.bootstrapAddrInfos {
		if pm.host.Network().Connectedness(pi.ID) == network.Connected {
			pm.logger.Printf("Client P2P: Already connected to bootstrap peer: %s", pi.ID)
			continue
		}

		wg.Add(1)
		go func(peerInfo peer.AddrInfo) {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-connectCtx.Done(): // Check if overall context for this phase is done
				return
			}

			// Use a per-peer timeout that respects the overall connectCtx
			peerConnectCtx, peerCancel := context.WithTimeout(connectCtx, 30*time.Second)
			defer peerCancel()

			pm.logger.Printf("Client P2P: Attempting to connect to bootstrap peer: %s", peerInfo.ID)
			if err := pm.host.Connect(peerConnectCtx, peerInfo); err != nil {
				// Log error only if the connection attempt itself failed, not if the context was cancelled
				if connectCtx.Err() == nil && peerConnectCtx.Err() == nil {
					pm.logger.Printf("Client P2P: Failed to connect to bootstrap peer %s: %v", peerInfo.ID, err)
				}
			} else {
				pm.logger.Printf("Client P2P: Successfully connected to bootstrap peer: %s", peerInfo.ID.String())
			}
		}(pi)
	}

	waitChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitChan)
	}()

	select {
	case <-waitChan:
		pm.logger.Println("Client P2P: Bootstrap connection attempts phase completed.")
	case <-connectCtx.Done(): // This is the timeout for the entire connectToBootstrapPeers operation
		pm.logger.Printf("Client P2P: Bootstrap connection attempts phase ended: %v", connectCtx.Err())
	}
}

func (pm *P2PManager) serverConnectionManager() {
	defer pm.shutdownWg.Done()
	defer pm.logger.Println("Client P2P: Server connection manager stopped")

	if pm.serverPeerIDStr == "" {
		pm.logger.Println("Client P2P: No server PeerID configured. Server connection manager will not run.")
		return
	}
	targetPeerID, err := peer.Decode(pm.serverPeerIDStr)
	if err != nil {
		pm.logger.Printf("Client P2P ERROR: Invalid server PeerID '%s': %v. Server connection manager will not run.", pm.serverPeerIDStr, err)
		return
	}

	// Initial check fairly quickly after startup, once bootstrapManager has had a chance to connect
	initialCheckTimer := time.NewTimer(30 * time.Second)
	defer initialCheckTimer.Stop()

	// Regular health/reconnect ticker
	ticker := time.NewTicker(pm.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-initialCheckTimer.C:
			if pm.ctx.Err() == nil && pm.shouldAttemptConnection(targetPeerID) {
				pm.findAndConnectToServer(pm.ctx, targetPeerID)
			}
		case <-ticker.C:
			if pm.ctx.Err() == nil && pm.shouldAttemptConnection(targetPeerID) {
				pm.findAndConnectToServer(pm.ctx, targetPeerID)
			}
		}
	}
}

func (pm *P2PManager) shouldAttemptConnection(targetPeerID peer.ID) bool {
	if pm.host.Network().Connectedness(targetPeerID) == network.Connected {
		// If connected, but no contact for a while, we might want to "ping" or re-verify.
		// For now, if connected, assume it's fine until healthCheckInterval.
		// SendLogBatch updates lastServerContact. If no batches sent, this check is useful.
		if time.Since(pm.lastServerContact) < pm.healthCheckInterval*2 { // Allow more grace if connected
			return false
		}
		pm.logger.Printf("Client P2P: Connected to server %s, but last contact was > %v ago. Will re-verify/attempt.", targetPeerID, pm.healthCheckInterval*2)
	}

	_, _, _, consecutiveFailures, _, lastFailureTime := pm.connectionStats.GetStats()
	if consecutiveFailures > 0 {
		if time.Since(lastFailureTime) < pm.backoffManager.GetDelay() {
			// Optionally log that we are waiting for backoff.
			// pm.logger.Printf("Client P2P: In backoff period for server %s. Time remaining: %v", targetPeerID, pm.backoffManager.GetDelay()-time.Since(lastFailureTime))
			return false
		}
	}
	return true
}

func (pm *P2PManager) findAndConnectToServer(ctx context.Context, targetPeerID peer.ID) {
	// Prevent concurrent findAndConnectToServer attempts
	if !pm.connectionState.CompareAndSwap(int32(StateDisconnected), int32(StateConnecting)) &&
		!pm.connectionState.CompareAndSwap(int32(StateFailed), int32(StateConnecting)) {
		if pm.connectionState.Load() == int32(StateConnecting) {
			pm.logger.Println("Client P2P: findAndConnectToServer: Connection attempt already in progress.")
		}
		return
	}
	// Ensure state is reset if this function returns early without success/failure paths being hit
	defer func() {
		// If we are still 'Connecting' but didn't reach a success/failure path that sets another state.
		pm.connectionState.CompareAndSwap(int32(StateConnecting), int32(StateFailed))
	}()

	pm.logger.Printf("Client P2P: Attempting to find and connect to server %s...", targetPeerID)

	// Overall timeout for this entire find and connect operation
	findConnectCtx, findConnectCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer findConnectCancel()

	// 1. Try Peerstore addresses
	if addrs := pm.host.Peerstore().Addrs(targetPeerID); len(addrs) > 0 {
		pm.logger.Printf("Client P2P: Server %s has %d addresses in peerstore. Attempting direct connect.", targetPeerID, len(addrs))
		directConnectCtx, cancelDirect := context.WithTimeout(findConnectCtx, 30*time.Second) // Shorter timeout for direct
		err := pm.host.Connect(directConnectCtx, peer.AddrInfo{ID: targetPeerID, Addrs: addrs})
		cancelDirect()
		if err == nil {
			pm.onConnectionSuccess(targetPeerID)
			return
		}
		pm.logger.Printf("Client P2P: Direct connection to %s using cached addrs failed: %v. Trying other methods.", targetPeerID, err)
	} else {
		pm.logger.Printf("Client P2P: No cached addresses for server %s in peerstore.", targetPeerID)
	}
	if findConnectCtx.Err() != nil {
		return
	} // Check for timeout/cancellation

	// 2. Try Rendezvous (if routingDiscovery is available)
	if pm.routingDiscovery != nil {
		pm.logger.Printf("Client P2P: Attempting to find server %s via rendezvous...", targetPeerID)
		// findServerViaRendezvous already has internal timeouts
		if pm.findServerViaRendezvous(findConnectCtx, targetPeerID) {
			if pm.host.Network().Connectedness(targetPeerID) == network.Connected {
				pm.onConnectionSuccess(targetPeerID)
				return
			}
			pm.logger.Printf("Client P2P: Rendezvous found server %s, but not connected post-discovery. Will proceed to DHT FindPeer.", targetPeerID)
		} else {
			pm.logger.Printf("Client P2P: Server %s not found or connected via rendezvous.", targetPeerID)
		}
	} else {
		pm.logger.Println("Client P2P: RoutingDiscovery not available, skipping rendezvous.")
	}
	if findConnectCtx.Err() != nil {
		return
	}

	// 3. Try DHT FindPeer
	pm.logger.Printf("Client P2P: Attempting DHT FindPeer for server %s...", targetPeerID)
	findPeerCtx, findPeerCancel := context.WithTimeout(findConnectCtx, 1*time.Minute) // Timeout for FindPeer
	peerInfo, err := pm.kademliaDHT.FindPeer(findPeerCtx, targetPeerID)
	findPeerCancel()

	if err != nil {
		pm.onConnectionFailure(targetPeerID, fmt.Errorf("all discovery methods failed; DHT FindPeer for %s error: %w", targetPeerID, err))
		return
	}
	pm.logger.Printf("Client P2P: Found server %s via DHT at addrs: %v", targetPeerID, peerInfo.Addrs)

	if len(peerInfo.Addrs) == 0 {
		pm.onConnectionFailure(targetPeerID, fmt.Errorf("DHT FindPeer for %s succeeded but returned no addresses", targetPeerID))
		return
	}
	pm.host.Peerstore().AddAddrs(peerInfo.ID, peerInfo.Addrs, peerstoreTTL) // Add fresh addrs

	pm.logger.Printf("Client P2P: Attempting to connect to server %s using addresses from DHT.", targetPeerID)
	connectDHTCtx, connectDHTCancel := context.WithTimeout(findConnectCtx, 45*time.Second) // Timeout for connect
	defer connectDHTCancel()
	if err := pm.host.Connect(connectDHTCtx, peerInfo); err != nil {
		pm.onConnectionFailure(targetPeerID, fmt.Errorf("dial to %s (addrs from DHT) failed: %w", targetPeerID, err))
		return
	}

	pm.onConnectionSuccess(targetPeerID)
}

func (pm *P2PManager) findServerViaRendezvous(ctx context.Context, targetPeerID peer.ID) bool {
	if pm.routingDiscovery == nil {
		pm.logger.Println("Client P2P: RoutingDiscovery not initialized, cannot use rendezvous")
		return false
	}
	pm.logger.Printf("Client P2P: Searching for servers via rendezvous: %s", ServerRendezvousString)

	// Use a context with a specific timeout for this discovery operation
	discoverCtx, cancelDiscover := context.WithTimeout(ctx, 45*time.Second) // Rendezvous can take time
	defer cancelDiscover()

	peerChan, err := pm.routingDiscovery.FindPeers(discoverCtx, ServerRendezvousString)
	if err != nil {
		if discoverCtx.Err() == nil {
			pm.logger.Printf("Client P2P: Failed to start peer discovery for rendezvous '%s': %v", ServerRendezvousString, err)
		}
		return false
	}

	foundTargetAndConnected := false
	for {
		select {
		case <-discoverCtx.Done():
			pm.logger.Printf("Client P2P: Rendezvous discovery for '%s' ended: %v. Target connected: %v", ServerRendezvousString, discoverCtx.Err(), foundTargetAndConnected)
			return foundTargetAndConnected
		case pi, ok := <-peerChan:
			if !ok { // Channel closed
				pm.logger.Printf("Client P2P: Rendezvous peer channel closed for '%s'. Target connected: %v", ServerRendezvousString, foundTargetAndConnected)
				return foundTargetAndConnected
			}
			if pi.ID == "" {
				continue
			} // Skip empty peer info

			pm.logger.Printf("Client P2P: Found peer via rendezvous: %s (%d addrs)", pi.ID, len(pi.Addrs))

			if pi.ID == targetPeerID {
				pm.logger.Printf("Client P2P: Found target server %s via rendezvous!", targetPeerID)
				if len(pi.Addrs) > 0 {
					pm.host.Peerstore().AddAddrs(pi.ID, pi.Addrs, peerstoreTTL)
					pm.logger.Printf("Client P2P: Added/updated addresses for target server %s in peerstore.", targetPeerID)

					// Attempt to connect immediately upon finding the target via rendezvous
					connectRdzCtx, connectRdzCancel := context.WithTimeout(discoverCtx, 20*time.Second)
					if err := pm.host.Connect(connectRdzCtx, pi); err == nil {
						pm.logger.Printf("Client P2P: Successfully connected to server %s found via rendezvous.", targetPeerID)
						foundTargetAndConnected = true
					} else {
						if connectRdzCtx.Err() == nil {
							pm.logger.Printf("Client P2P: Failed to connect to server %s (found via rendezvous): %v", targetPeerID, err)
						}
					}
					connectRdzCancel()
					if foundTargetAndConnected {
						return true
					} // If connected, can exit early
				} else {
					pm.logger.Printf("Client P2P: Target server %s found via rendezvous but no addresses advertised with it.", targetPeerID)
				}
			}
		}
	}
}

func (pm *P2PManager) onConnectionSuccess(peerID peer.ID) {
	pm.connectionState.Store(int32(StateConnected))
	pm.connectionStats.RecordAttempt(true)
	pm.lastServerContact = time.Now()
	pm.logger.Printf("Client P2P: Successfully connected to server %s", peerID)
}

func (pm *P2PManager) onConnectionFailure(peerID peer.ID, err error) {
	pm.connectionState.Store(int32(StateFailed))
	pm.connectionStats.RecordAttempt(false)
	if pm.ctx.Err() == nil { // Log only if not due to overall shutdown
		retryDelay := pm.backoffManager.GetDelay()
		pm.logger.Printf("Client P2P: Failed to connect/find server %s: %v (will retry after ~%s if still disconnected)", peerID, err, retryDelay)
	}
}

func (pm *P2PManager) addressMonitor() {
	defer pm.shutdownWg.Done()
	defer pm.logger.Println("Client P2P: Address monitor stopped")

	addrSub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated))
	if err != nil {
		pm.logger.Printf("Client P2P ERROR: Failed to subscribe to address updates: %v", err)
		return
	}
	defer addrSub.Close()

	reachabilitySub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		pm.logger.Printf("Client P2P ERROR: Failed to subscribe to reachability events: %v", err)
	} else {
		defer reachabilitySub.Close()
	}

	pm.logger.Printf("Client P2P: Monitoring address and NAT changes. Initial Listen Addresses: %v", pm.host.Addrs())

	for {
		select {
		case <-pm.ctx.Done():
			return
		case ev, ok := <-addrSub.Out():
			if !ok {
				pm.logger.Println("Client P2P: Address subscription closed.")
				addrSub = nil
				if reachabilitySub == nil {
					return
				} // exit if both subs closed
				continue
			}
			addressEvent := ev.(event.EvtLocalAddressesUpdated)
			pm.logger.Printf("Client P2P: Host addresses updated.")
			for _, currentAddr := range addressEvent.Current {
				pm.logger.Printf("Client P2P:  -> Current: %s (Action: %s)", currentAddr.Address, currentAddr.Action)
			}
			for _, removedAddr := range addressEvent.Removed {
				pm.logger.Printf("Client P2P:  -> Removed: %s", removedAddr.Address)
			}
		case ev, ok := <-reachabilitySub.Out():
			if !ok {
				pm.logger.Println("Client P2P: Reachability subscription closed.")
				reachabilitySub = nil
				if addrSub == nil {
					return
				} // exit if both subs closed
				continue
			}
			reachabilityEvent := ev.(event.EvtLocalReachabilityChanged)
			pm.logger.Printf("Client P2P: Reachability status changed to: %s", reachabilityEvent.Reachability.String())
			if reachabilityEvent.Reachability == network.ReachabilityPublic {
				pm.logger.Println("Client P2P: Client reachability is PUBLIC. Good for hole punching.")
			}
		}
	}
}

func (pm *P2PManager) cleanupManager() {
	defer pm.shutdownWg.Done()
	defer pm.logger.Println("Client P2P: Cleanup manager stopped")
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-ticker.C:
			if pm.streamPool != nil {
				pm.streamPool.Cleanup()
			}
		}
	}
}

func (pm *P2PManager) SendLogBatch(ctx context.Context, clientAppID string, encryptedPayload []byte) (*p2p.LogBatchResponse, error) {
	select { // Rate limiting
	case pm.rateLimiter <- struct{}{}:
		defer func() { <-pm.rateLimiter }()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Second): // Increased timeout for acquiring rate limit slot
		return nil, fmt.Errorf("rate limit timeout: too many concurrent SendLogBatch requests")
	}

	if pm.serverPeerIDStr == "" {
		return nil, fmt.Errorf("server PeerID not configured")
	}
	targetPeerID, err := peer.Decode(pm.serverPeerIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid server PeerID '%s': %w", pm.serverPeerIDStr, err)
	}

	if pm.host.Network().Connectedness(targetPeerID) != network.Connected {
		pm.logger.Printf("Client P2P: Not connected to server %s for SendLogBatch. Connection manager should handle reconnection. Current state: %d", targetPeerID, pm.connectionState.Load())
		// Trigger a connection attempt if appropriate, but be careful not to spam
		if pm.shouldAttemptConnection(targetPeerID) {
			go pm.findAndConnectToServer(pm.ctx, targetPeerID) // Non-blocking attempt
		}
		return nil, fmt.Errorf("not connected to server %s", targetPeerID)
	}

	// Use a context for the stream opening and operations
	streamOpCtx, cancelStreamOp := context.WithTimeout(ctx, 75*time.Second) // Overall timeout for send and receive
	defer cancelStreamOp()

	stream, err := pm.host.NewStream(streamOpCtx, targetPeerID, p2p.ProtocolID)
	if err != nil {
		return nil, fmt.Errorf("failed to open new stream to server %s: %w", targetPeerID, err)
	}
	pm.logger.Printf("Client P2P: Created new stream to server %s for LogBatch", targetPeerID)
	defer stream.Reset() // Ensures stream is reset/closed

	req := p2p.LogBatchRequest{
		Header:              p2p.NewMessageHeader(p2p.MessageTypeLogBatch),
		AppClientID:         clientAppID,
		EncryptedLogPayload: encryptedPayload,
	}

	// Write request with timeout
	if err := p2p.WriteMessageWithTimeout(stream, &req, 30*time.Second); err != nil {
		return nil, fmt.Errorf("failed to write LogBatchRequest (ReqID: %s): %w", req.Header.RequestID, err)
	}

	// Read response with timeout
	var resp p2p.LogBatchResponse
	if err := p2p.ReadMessageWithTimeout(stream, &resp, 60*time.Second); err != nil {
		return nil, fmt.Errorf("failed to read LogBatchResponse (for ReqID: %s): %w", req.Header.RequestID, err)
	}

	if resp.Header.MessageType != p2p.MessageTypeLogBatch && resp.Header.MessageType != p2p.MessageTypeError {
		return nil, fmt.Errorf("unexpected response message type: %s (RequestID: %s)", resp.Header.MessageType, req.Header.RequestID)
	}
	if resp.Header.RequestID != req.Header.RequestID {
		return nil, fmt.Errorf("response RequestID mismatch: expected %s, got %s", req.Header.RequestID, resp.Header.RequestID)
	}

	pm.lastServerContact = time.Now()
	pm.logger.Printf("Client P2P: Successfully sent batch (ReqID: %s) and received response from server %s. Updated last contact time.", req.Header.RequestID, targetPeerID)
	return &resp, nil
}

func (pm *P2PManager) GetConnectionStats() (ConnectionState, *ConnectionStats) {
	state := ConnectionState(pm.connectionState.Load())
	return state, pm.connectionStats
}

func (pm *P2PManager) Close() error {
	pm.logger.Println("Client P2P: Shutting down P2P Manager...")
	pm.cancel() // Signal all goroutines using pm.ctx to stop

	pm.logger.Println("Client P2P: Waiting for background goroutines to complete...")
	pm.shutdownWg.Wait() // Wait for goroutines to finish

	if pm.mdnsService != nil {
		pm.logger.Println("Client P2P: Closing mDNS service...")
		if err := pm.mdnsService.Close(); err != nil {
			pm.logger.Printf("Client P2P: Error closing mDNS service: %v", err)
		}
	}

	var Rerr error
	if pm.host != nil {
		pm.logger.Println("Client P2P: Closing libp2p host...")
		if err := pm.host.Close(); err != nil {
			pm.logger.Printf("Client P2P: Error closing libp2p host: %v", err)
			Rerr = err
		}
	}
	pm.logger.Println("Client P2P: P2P Manager shut down complete.")
	return Rerr
}

func runP2PManager(p2pManager *P2PManager, internalQuit <-chan struct{}, wg *sync.WaitGroup, logger *log.Logger) {
	defer wg.Done()
	p2pManager.logger.Println("Client P2P Manager controlling goroutine starting.")
	p2pManager.StartDiscoveryAndBootstrap() // This uses p2pManager.ctx

	select {
	case <-internalQuit:
		p2pManager.logger.Println("Client P2P Manager: Received internal quit signal. Initiating shutdown...")
	case <-p2pManager.ctx.Done(): // If P2P manager's internal context is cancelled (e.g. unrecoverable error)
		p2pManager.logger.Println("Client P2P Manager: Internal P2P context done. Initiating shutdown...")
	}

	if err := p2pManager.Close(); err != nil {
		p2pManager.logger.Printf("Client P2P Manager: Error during Close: %v", err)
	}
	p2pManager.logger.Println("Client P2P Manager controlling goroutine finished.")
}
