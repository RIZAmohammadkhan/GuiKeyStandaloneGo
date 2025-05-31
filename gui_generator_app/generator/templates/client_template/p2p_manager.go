// GuiKeyStandaloneGo/generator/templates/client_template/p2p_manager.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/pkg/p2p" // Your P2P protocol definitions

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"

	// mDNS can be added back if desired for pure LAN fallback
	// "github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	routd "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	// AutoRelay import for options
)

type P2PManager struct {
	host             host.Host
	kademliaDHT      *dht.IpfsDHT
	routingDiscovery *routd.RoutingDiscovery
	// AutoNAT client is managed by the host automatically
	logger             *log.Logger
	serverPeerIDStr    string
	bootstrapAddrInfos []peer.AddrInfo
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
			logger.Printf("P2P Client WARN: Invalid bootstrap address string '%s': %v", addrStr, err)
			continue
		}
		bootAddrsInfo = append(bootAddrsInfo, *ai)
	}
	if len(bootAddrsInfo) == 0 {
		logger.Println("P2P Client WARN: No valid bootstrap addresses configured. DHT discovery will be severely limited unless on LAN with mDNS.")
	}

	var opts []libp2p.Option
	opts = append(opts, libp2p.ListenAddrStrings(
		"/ip4/0.0.0.0/tcp/0",
		"/ip4/0.0.0.0/udp/0/quic-v1", // Enable QUIC
	))
	opts = append(opts, libp2p.EnableRelay())
	opts = append(opts, libp2p.EnableHolePunching())
	// AutoNAT is enabled by default in newer versions, no explicit call needed

	if len(bootAddrsInfo) > 0 {
		// Fixed: EnableAutoRelayWithStaticRelays now requires autorelay options
		opts = append(opts, libp2p.EnableAutoRelayWithStaticRelays(bootAddrsInfo))
	} else {
		opts = append(opts, libp2p.EnableAutoRelay())
	}

	var kadDHT *dht.IpfsDHT
	opts = append(opts,
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			var err error
			dhtOpts := []dht.Option{dht.Mode(dht.ModeClient)}
			// DHT will use connected peers (like bootstrap) automatically,
			// but explicitly providing them to dht.New can also be done.
			// dht.BootstrapPeers is good for the initial bootstrap call later.
			kadDHT, err = dht.New(context.Background(), h, dhtOpts...)
			return kadDHT, err
		}),
	)

	p2pHost, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("p2p client: failed to create libp2p host: %w", err)
	}
	logger.Printf("P2P Client: Host created with ID: %s", p2pHost.ID().String())

	if kadDHT == nil {
		p2pHost.Close()
		return nil, fmt.Errorf("p2p client: Kademlia DHT was not initialized during host setup")
	}
	logger.Println("P2P Client: AutoNAT service and Kademlia DHT initialized with host.")

	return &P2PManager{
		host:               p2pHost,
		kademliaDHT:        kadDHT,
		logger:             logger,
		serverPeerIDStr:    serverPeerID,
		bootstrapAddrInfos: bootAddrsInfo,
	}, nil
}

func (pm *P2PManager) StartDiscoveryAndBootstrap(ctx context.Context) {
	pm.logger.Println("P2P Client: Starting discovery and bootstrap processes...")

	if len(pm.bootstrapAddrInfos) == 0 {
		pm.logger.Println("P2P Client: No bootstrap peers configured. DHT may not function effectively.")
		// Consider if mDNS should be started as a fallback for LAN here
		// pm.startMDNS(ctx)
		return // Still start other goroutines like findServer, address monitor
	}

	// Initial connection attempt to bootstrap peers
	pm.connectToBootstrapPeers(ctx)

	// Goroutine for periodic bootstrap retries and DHT re-bootstrap
	go func() {
		// Short initial delay, then longer periodic bootstraps
		initialBootstrapTimer := time.NewTimer(30 * time.Second) // Increased initial delay
		defer initialBootstrapTimer.Stop()

		bootstrapRetryTicker := time.NewTicker(5 * time.Minute)
		defer bootstrapRetryTicker.Stop()
		dhtReBootstrapTicker := time.NewTicker(20 * time.Minute) // Less frequent re-bootstrap
		defer dhtReBootstrapTicker.Stop()

		firstBootstrapDone := false

		for {
			select {
			case <-ctx.Done():
				pm.logger.Println("P2P Client: Bootstrap/Discovery supervisor goroutine stopping.")
				return
			case <-initialBootstrapTimer.C:
				if !firstBootstrapDone {
					pm.logger.Println("P2P Client: Attempting initial Kademlia DHT bootstrap...")
					if err := pm.kademliaDHT.Bootstrap(ctx); err != nil {
						pm.logger.Printf("P2P Client: Initial Kademlia DHT bootstrap failed: %v (will retry via ticker)", err)
					} else {
						pm.logger.Println("P2P Client: Initial Kademlia DHT bootstrap process completed/initiated.")
					}
					pm.routingDiscovery = routd.NewRoutingDiscovery(pm.kademliaDHT)
					firstBootstrapDone = true
				}
			case <-bootstrapRetryTicker.C:
				pm.logger.Println("P2P Client: Periodically retrying connections to bootstrap peers...")
				pm.connectToBootstrapPeers(ctx)
			case <-dhtReBootstrapTicker.C:
				pm.logger.Println("P2P Client: Periodically re-bootstrapping Kademlia DHT...")
				if err := pm.kademliaDHT.Bootstrap(ctx); err != nil { // Bootstrap again
					pm.logger.Printf("P2P Client: Periodic Kademlia DHT bootstrap failed: %v", err)
				} else {
					pm.logger.Println("P2P Client: Periodic Kademlia DHT bootstrap initiated.")
				}
			}
		}
	}()

	go pm.periodicallyFindServer(ctx)
	go pm.monitorAddressAndNATChanges(ctx)
}

func (pm *P2PManager) connectToBootstrapPeers(ctx context.Context) {
	if len(pm.bootstrapAddrInfos) == 0 {
		return
	}
	pm.logger.Printf("P2P Client: Attempting to connect to %d bootstrap peers if not already connected...", len(pm.bootstrapAddrInfos))
	var wg sync.WaitGroup
	for _, pi := range pm.bootstrapAddrInfos {
		if pm.host.Network().Connectedness(pi.ID) == network.Connected {
			continue // Skip if already connected
		}
		wg.Add(1)
		go func(peerInfo peer.AddrInfo) {
			defer wg.Done()
			connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := pm.host.Connect(connectCtx, peerInfo); err != nil {
				// These logs can be noisy if bootstrap nodes are flaky, consider reducing verbosity for repeated failures
				// pm.logger.Printf("P2P Client: Failed to connect to bootstrap peer %s (%v): %v", peerInfo.ID, peerInfo.Addrs, err)
			} else {
				pm.logger.Printf("P2P Client: Successfully connected to bootstrap peer: %s", peerInfo.ID.String())
			}
		}(pi)
	}
	// Don't wait with wg.Wait() to allow them to connect in the background
}

func (pm *P2PManager) monitorAddressAndNATChanges(ctx context.Context) {
	addrSub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated))
	if err != nil {
		pm.logger.Printf("P2P Client: Failed to subscribe to address update events: %v", err)
		return
	}
	defer addrSub.Close()

	// Subscribe to NAT status change events (reachability events)
	reachabilitySub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		pm.logger.Printf("P2P Client: Failed to subscribe to reachability events: %v", err)
	} else {
		defer reachabilitySub.Close()
	}

	pm.logger.Printf("P2P Client: Initial Listen Addresses: %v", pm.host.Addrs())

	for {
		select {
		case <-ctx.Done():
			pm.logger.Println("P2P Client: Address/NAT monitor stopping.")
			return
		case ev, ok := <-addrSub.Out():
			if !ok {
				addrSub = nil
				if reachabilitySub == nil {
					return
				}
				continue
			}
			addressEvent := ev.(event.EvtLocalAddressesUpdated)
			// Fixed: Use Current and Diffs instead of Listen (which doesn't exist)
			pm.logger.Printf("P2P Client: Host addresses updated. Current: %v. Diffs: %+v", addressEvent.Current, addressEvent.Diffs)
		case ev, ok := <-reachabilitySub.Out():
			if !ok {
				reachabilitySub = nil
				if addrSub == nil {
					return
				}
				continue
			}
			// Fixed: Use EvtLocalReachabilityChanged instead of autonat.Event
			reachabilityEvent := ev.(event.EvtLocalReachabilityChanged)
			pm.logger.Printf("P2P Client: Reachability status changed: %s", reachabilityEvent.Reachability.String())
		}
	}
}

func (pm *P2PManager) periodicallyFindServer(ctx context.Context) {
	if pm.serverPeerIDStr == "" {
		return
	}
	targetPeerID, err := peer.Decode(pm.serverPeerIDStr)
	if err != nil {
		pm.logger.Printf("P2P Client: Invalid server PeerID for periodic find: %v", err)
		return
	}

	ticker := time.NewTicker(1 * time.Minute) // Check server reachability more often
	defer ticker.Stop()

	// Initial attempt
	pm.findAndConnectToServer(ctx, targetPeerID)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pm.findAndConnectToServer(ctx, targetPeerID)
		}
	}
}

func (pm *P2PManager) findAndConnectToServer(ctx context.Context, targetPeerID peer.ID) {
	if pm.host.Network().Connectedness(targetPeerID) == network.Connected {
		return
	}
	pm.logger.Printf("P2P Client: Attempting to find server %s via DHT...", targetPeerID)
	findCtx, findCancel := context.WithTimeout(ctx, 45*time.Second)
	defer findCancel()

	peerInfo, err := pm.kademliaDHT.FindPeer(findCtx, targetPeerID)
	if err != nil {
		pm.logger.Printf("P2P Client: Could not find server %s via DHT: %v", targetPeerID, err)
		return
	}
	pm.logger.Printf("P2P Client: Found server %s via DHT at addrs: %v. Attempting connect.", targetPeerID, peerInfo.Addrs)
	pm.host.Peerstore().AddAddrs(peerInfo.ID, peerInfo.Addrs, 2*time.Hour)

	connectCtx, connectCancel := context.WithTimeout(ctx, 30*time.Second)
	defer connectCancel()
	err = pm.host.Connect(connectCtx, peerInfo)
	if err != nil {
		pm.logger.Printf("P2P Client: Failed to connect to server %s found via DHT: %v", targetPeerID, err)
	} else {
		pm.logger.Printf("P2P Client: Successfully connected to server %s found via DHT.", targetPeerID)
	}
}

func (pm *P2PManager) SendLogBatch(ctx context.Context, clientAppID string, encryptedPayload []byte) (*p2p.LogBatchResponse, error) {
	if pm.serverPeerIDStr == "" {
		return nil, fmt.Errorf("p2p client: server PeerID is not configured")
	}
	targetPeerID, err := peer.Decode(pm.serverPeerIDStr)
	if err != nil {
		return nil, fmt.Errorf("p2p client: invalid server PeerID string '%s': %w", pm.serverPeerIDStr, err)
	}

	pm.logger.Printf("P2P Client: Attempting to open stream to server %s for protocol %s", targetPeerID, p2p.ProtocolID)
	streamCtx, cancelStream := context.WithTimeout(ctx, 30*time.Second)
	defer cancelStream()

	stream, err := pm.host.NewStream(streamCtx, targetPeerID, p2p.ProtocolID)
	if err != nil {
		return nil, fmt.Errorf("p2p client: failed to open new stream to server %s: %w", targetPeerID, err)
	}
	defer stream.Reset()
	pm.logger.Printf("P2P Client: Stream opened to server %s", targetPeerID)

	_ = stream.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_ = stream.SetReadDeadline(time.Now().Add(60 * time.Second))

	response, err := p2p.SendLogBatchAndGetResponse(stream, clientAppID, encryptedPayload)
	if err != nil {
		return nil, fmt.Errorf("p2p client: error during log batch send/receive: %w", err)
	}

	pm.logger.Printf("P2P Client: Received response from server: Status='%s', Msg='%s', Processed=%d",
		response.Status, response.Message, response.EventsProcessed)
	return response, nil
}

func (pm *P2PManager) Close() error {
	// AutoNAT client associated with the host is closed when the host is closed.
	// No explicit autoNATClient.Close() needed.
	if pm.kademliaDHT != nil {
		if err := pm.kademliaDHT.Close(); err != nil {
			pm.logger.Printf("P2P Client: Error closing Kademlia DHT: %v", err)
			// Don't return early, try to close host too
		}
	}
	if pm.host != nil {
		pm.logger.Println("P2P Client: Closing libp2p host...")
		return pm.host.Close()
	}
	return nil
}

func runP2PManager(p2pManager *P2PManager, internalQuit <-chan struct{}, wg *sync.WaitGroup, logger *log.Logger) {
	defer wg.Done()
	// Use the P2PManager's own logger
	p2pManager.logger.Println("P2P Client Manager goroutine starting.")

	p2pCtx, cancelP2pCtx := context.WithCancel(context.Background())
	defer cancelP2pCtx()

	p2pManager.StartDiscoveryAndBootstrap(p2pCtx)

	<-internalQuit
	p2pManager.logger.Println("P2P Client Manager goroutine stopping...")
}
