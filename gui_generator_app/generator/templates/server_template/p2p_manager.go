// GuiKeyStandaloneGo/generator/templates/server_template/p2p_manager.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
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
	"github.com/libp2p/go-libp2p/core/peer"
	coreRouting "github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	routd "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"

	"github.com/multiformats/go-multiaddr"
)

const ServerRendezvousString = "guikey-standalone-logserver/v1.0.0"
const LocalDiscoveryServiceTag = "guikey-standalone-logserver-local" // For mDNS

// CfgExplicitP2PPort will be defined in config_generated.go (var CfgExplicitP2PPort uint16)
// If it's 0, dynamic ports are used for TCP. QUIC is always dynamic.

type ServerP2PManager struct {
	host               host.Host
	kademliaDHT        *dht.IpfsDHT
	routingDiscovery   *routd.RoutingDiscovery
	mdnsService        mdns.Service
	logger             *log.Logger
	serverLogStore     *ServerLogStore
	encryptionKeyHex   string
	bootstrapAddrInfos []peer.AddrInfo
	p2pCtx             context.Context    // Main context for P2P operations
	p2pCancel          context.CancelFunc // To cancel P2P operations
	shutdownWg         sync.WaitGroup     // For graceful shutdown of goroutines started by P2PManager
}

type mdnsNotifee struct {
	logger *log.Logger
	h      host.Host
}

func (m *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	m.logger.Printf("Server P2P (mDNS): Discovered local peer %s: %v", pi.ID, pi.Addrs)
	// Server typically doesn't need to connect back, but good to know who's around.
	// If you want to store addresses in peerstore, use your own TTL instead of mdns.DefaultTTL (removed):
	//    m.h.Peerstore().AddAddrs(pi.ID, pi.Addrs, peerstoreTTL)
}

func NewServerP2PManager(
	logger *log.Logger,
	_ string, // Old CfgP2PListenAddress, now unused as port comes from CfgExplicitP2PPort
	identitySeedHex string,
	bootstrapAddrs []string,
	serverLogStore *ServerLogStore,
	appEncryptionKeyHex string,
) (*ServerP2PManager, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "SERVER_P2P_FALLBACK: ", log.LstdFlags|log.Lshortfile)
	}

	privKey, _, err := pkgCrypto.Libp2pKeyFromSeedHex(identitySeedHex)
	if err != nil {
		return nil, fmt.Errorf("server_p2p: failed to create identity from seed: %w", err)
	}

	var bootAddrsInfo []peer.AddrInfo
	for _, addrStr := range bootstrapAddrs {
		if addrStr == "" {
			continue
		}
		ai, errParse := peer.AddrInfoFromString(addrStr)
		if errParse != nil {
			logger.Printf("Server P2P WARN: Invalid bootstrap address string '%s': %v", addrStr, errParse)
			continue
		}
		bootAddrsInfo = append(bootAddrsInfo, *ai)
	}

	// Construct listen addresses based on CfgExplicitP2PPort
	var listenMas []multiaddr.Multiaddr
	if CfgExplicitP2PPort > 0 {
		tcp4Listen, errTCP4 := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", CfgExplicitP2PPort))
		if errTCP4 == nil {
			listenMas = append(listenMas, tcp4Listen)
		} else {
			logger.Printf("Server P2P ERROR: Could not create IPv4 TCP listen MA for port %d: %v. Using dynamic.", CfgExplicitP2PPort, errTCP4)
			listenMas = append(listenMas, multiaddr.StringCast("/ip4/0.0.0.0/tcp/0"))
		}
		tcp6Listen, errTCP6 := multiaddr.NewMultiaddr(fmt.Sprintf("/ip6/::/tcp/%d", CfgExplicitP2PPort))
		if errTCP6 == nil {
			listenMas = append(listenMas, tcp6Listen)
		} else {
			logger.Printf("Server P2P WARN: Could not create IPv6 TCP listen MA for port %d: %v. Using dynamic for IPv6 TCP.", CfgExplicitP2PPort, errTCP6)
			listenMas = append(listenMas, multiaddr.StringCast("/ip6/::/tcp/0"))
		}
		logger.Printf("Server P2P: Configured to listen on specific TCP port: %d (IPv4/IPv6)", CfgExplicitP2PPort)
	} else {
		listenMas = append(listenMas, multiaddr.StringCast("/ip4/0.0.0.0/tcp/0")) // Dynamic IPv4 TCP
		listenMas = append(listenMas, multiaddr.StringCast("/ip6/::/tcp/0"))      // Dynamic IPv6 TCP
		logger.Println("Server P2P: Configured to listen on dynamic TCP ports.")
	}
	// Always add dynamic QUIC for robustness
	listenMas = append(listenMas, multiaddr.StringCast("/ip4/0.0.0.0/udp/0/quic-v1"))
	listenMas = append(listenMas, multiaddr.StringCast("/ip6/::/udp/0/quic-v1"))

	limiter := rcmgr.NewFixedLimiter(rcmgr.DefaultLimits.AutoScale())
	rm, err := rcmgr.NewResourceManager(limiter)
	if err != nil {
		return nil, fmt.Errorf("server_p2p: failed to create resource manager: %w", err)
	}

	p2pCtx, p2pCancel := context.WithCancel(context.Background())

	var kadDHT *dht.IpfsDHT
	opts := []libp2p.Option{
		libp2p.Identity(privKey),
		libp2p.ListenAddrs(listenMas...),
		libp2p.ResourceManager(rm),
		libp2p.Routing(func(h host.Host) (coreRouting.PeerRouting, error) {
			var dhtErr error
			dhtOpts := []dht.Option{
				dht.Mode(dht.ModeServer),
				dht.ProtocolPrefix("/p2p"),
				dht.QueryFilter(dht.PublicQueryFilter),
				dht.RoutingTableFilter(dht.PublicRoutingTableFilter),
			}
			if len(bootAddrsInfo) > 0 {
				dhtOpts = append(dhtOpts, dht.BootstrapPeers(bootAddrsInfo...))
			}
			kadDHT, dhtErr = dht.New(p2pCtx, h, dhtOpts...) // Use p2pCtx for DHT
			return kadDHT, dhtErr
		}),
		libp2p.EnableHolePunching(),
		libp2p.EnableRelayService(), // Server can act as a relay
		libp2p.EnableNATService(),
		libp2p.NATPortMap(), // Tries UPnP/NAT-PMP

		// Enable AutoRelay with the new autorelay options :contentReference[oaicite:2]{index=2}
		libp2p.EnableAutoRelay(
			autorelay.WithStaticRelays(bootAddrsInfo),
			autorelay.WithNumRelays(2),
			autorelay.WithBootDelay(90*time.Second),
		),

		libp2p.ForceReachabilityPrivate(), // Start assuming private, let AutoNAT discover public
	}

	p2pHost, err := libp2p.New(opts...)
	if err != nil {
		p2pCancel()
		return nil, fmt.Errorf("server_p2p: failed to create libp2p host: %w", err)
	}
	logger.Printf("Server P2P: Host created. ID: %s", p2pHost.ID().String())
	actualAddrs := p2pHost.Addrs()
	logger.Printf("Server P2P: Actually listening on addresses: %v", actualAddrs)
	if len(actualAddrs) == 0 {
		logger.Println("Server P2P WARNING: Host is not listening on any addresses. Check configurations and permissions.")
	}

	if kadDHT == nil {
		p2pHost.Close()
		p2pCancel()
		return nil, fmt.Errorf("server_p2p: Kademlia DHT was not initialized (nil after libp2p.New)")
	}
	logger.Println("Server P2P: Kademlia DHT initialized.")

	routingDiscovery := routd.NewRoutingDiscovery(kadDHT)

	// mDNS no longer returns an error; drop errMdns and just assign the Service :contentReference[oaicite:3]{index=3}
	mdnsSvc := mdns.NewMdnsService(p2pHost, LocalDiscoveryServiceTag, &mdnsNotifee{logger: logger, h: p2pHost})
	// If you'd like to log a warning if mdnsSvc is nil, you can check here. In practice, NewMdnsService always returns a non-nil Service.
	_ = mdnsSvc

	p2pHost.SetStreamHandler(p2p.ProtocolID, func(stream network.Stream) {
		handleIncomingLogStream(stream, logger, serverLogStore, appEncryptionKeyHex)
	})
	logger.Printf("Server P2P: Stream handler set for protocol ID: %s", p2p.ProtocolID)

	return &ServerP2PManager{
		host:               p2pHost,
		kademliaDHT:        kadDHT,
		routingDiscovery:   routingDiscovery,
		mdnsService:        mdnsSvc,
		logger:             logger,
		serverLogStore:     serverLogStore,
		encryptionKeyHex:   appEncryptionKeyHex,
		bootstrapAddrInfos: bootAddrsInfo,
		p2pCtx:             p2pCtx,
		p2pCancel:          p2pCancel,
	}, nil
}

func (pm *ServerP2PManager) StartBootstrapAndDiscovery() { // Removed context argument, uses internal pm.p2pCtx
	pm.logger.Println("Server P2P: Starting bootstrap, discovery, and advertising processes...")
	pm.shutdownWg.Add(3) // For bootstrapConnects, dhtAndAdvertise, addressMonitor

	// 1. Connect to Bootstrap Peers
	go func() {
		defer pm.shutdownWg.Done()
		pm.connectToBootstrapPeers(pm.p2pCtx)
	}()

	// 2. Bootstrap DHT and then start advertising
	go func() {
		defer pm.shutdownWg.Done()
		pm.bootstrapDHTAndAdvertise(pm.p2pCtx)
	}()

	// 3. Monitor address changes
	go func() {
		defer pm.shutdownWg.Done()
		pm.monitorAddressAndNATChanges(pm.p2pCtx)
	}()

	// 4. Start mDNS service (if initialized)
	if pm.mdnsService != nil {
		pm.shutdownWg.Add(1)
		go func() {
			defer pm.shutdownWg.Done()
			pm.logger.Println("Server P2P: Starting mDNS local discovery service...")
			if err := pm.mdnsService.Start(); err != nil {
				pm.logger.Printf("Server P2P WARN: Failed to start mDNS service: %v", err)
			} else {
				pm.logger.Println("Server P2P: mDNS local discovery service started.")
				<-pm.p2pCtx.Done() // Keep mDNS running until P2P manager context is cancelled
				pm.logger.Println("Server P2P: mDNS service stopping due to P2P context cancellation.")
			}
		}()
	}
}

func (pm *ServerP2PManager) connectToBootstrapPeers(ctx context.Context) {
	if len(pm.bootstrapAddrInfos) == 0 {
		pm.logger.Println("Server P2P: No bootstrap peers configured. DHT/Relay/AutoNAT will be severely limited.")
		return
	}
	pm.logger.Printf("Server P2P: Connecting to %d bootstrap peers...", len(pm.bootstrapAddrInfos))

	var wg sync.WaitGroup
	connectCtx, cancelAllConnects := context.WithTimeout(ctx, 2*time.Minute) // Overall timeout for all bootstrap connections
	defer cancelAllConnects()

	for _, pi := range pm.bootstrapAddrInfos {
		if pm.host.Network().Connectedness(pi.ID) == network.Connected {
			pm.logger.Printf("Server P2P: Already connected to bootstrap peer: %s", pi.ID)
			continue
		}
		wg.Add(1)
		go func(peerInfo peer.AddrInfo) {
			defer wg.Done()
			// Use a per-peer timeout that respects the overall connectCtx
			peerConnectCtx, peerCancel := context.WithTimeout(connectCtx, 45*time.Second)
			defer peerCancel()

			pm.logger.Printf("Server P2P: Attempting to connect to bootstrap peer: %s", peerInfo.ID)
			if err := pm.host.Connect(peerConnectCtx, peerInfo); err != nil {
				if connectCtx.Err() == nil && peerConnectCtx.Err() == nil { // Log only if not due to cancellation
					pm.logger.Printf("Server P2P: Failed to connect to bootstrap peer %s: %v", peerInfo.ID, err)
				}
			} else {
				pm.logger.Printf("Server P2P: Successfully connected to bootstrap peer: %s", peerInfo.ID.String())
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
		pm.logger.Println("Server P2P: Bootstrap connection attempts phase completed.")
	case <-connectCtx.Done():
		pm.logger.Printf("Server P2P: Bootstrap connection phase ended: %v", connectCtx.Err())
	}
}

func (pm *ServerP2PManager) bootstrapDHTAndAdvertise(ctx context.Context) {
	pm.logger.Println("Server P2P: Bootstrapping Kademlia DHT...")

	// Initial delay for network connections to potentially establish
	select {
	case <-time.After(20 * time.Second): // Increased initial delay
	case <-ctx.Done():
		pm.logger.Println("Server P2P: DHT bootstrap cancelled before starting.")
		return
	}

	dhtBootstrapCtx, dhtCancel := context.WithTimeout(ctx, 5*time.Minute) // Generous timeout for DHT bootstrap
	defer dhtCancel()
	if err := pm.kademliaDHT.Bootstrap(dhtBootstrapCtx); err != nil {
		if dhtBootstrapCtx.Err() == nil {
			pm.logger.Printf("Server P2P: Kademlia DHT bootstrap process failed: %v", err)
		}
	} else {
		pm.logger.Println("Server P2P: Kademlia DHT bootstrap process completed/initiated.")
	}

	// Wait for DHT to populate and AutoNAT to potentially run before advertising
	select {
	case <-time.After(90 * time.Second): // Give AutoNAT more time
	case <-ctx.Done():
		pm.logger.Println("Server P2P: Context done before rendezvous advertising could start.")
		return
	}

	if ctx.Err() == nil { // Only advertise if the main P2P context is still active
		pm.startRendezvousAdvertising(ctx)
	}
}

func (pm *ServerP2PManager) startRendezvousAdvertising(ctx context.Context) {
	if pm.routingDiscovery == nil {
		pm.logger.Println("Server P2P: RoutingDiscovery not initialized, cannot advertise via DHT rendezvous.")
		return
	}

	pm.logger.Printf("Server P2P: Starting to advertise self on DHT with rendezvous string: %s", ServerRendezvousString)
	pm.shutdownWg.Add(1) // Add to shutdown wait group for the advertising goroutine
	go func() {
		defer pm.shutdownWg.Done()
		pm.advertiseWithRetry(ctx, ServerRendezvousString) // Initial and periodic advertisement

		// Periodic re-advertisement
		advertiseTicker := time.NewTicker(8 * time.Minute) // Adjusted ticker
		defer advertiseTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				pm.logger.Printf("Server P2P: Stopping rendezvous advertisement for '%s' due to context cancellation.", ServerRendezvousString)
				return
			case <-advertiseTicker.C:
				if ctx.Err() != nil { // Check context before advertising
					return
				}
				pm.logger.Printf("Server P2P: Re-advertising self on DHT (Rendezvous: %s)", ServerRendezvousString)
				advCtx, cancelAdv := context.WithTimeout(ctx, 1*time.Minute)
				_, errD := pm.routingDiscovery.Advertise(advCtx, ServerRendezvousString)
				cancelAdv()
				if errD != nil {
					pm.logger.Printf("Server P2P: Error re-advertising self on DHT (Rendezvous: %s): %v", ServerRendezvousString, errD)
				} else {
					pm.logger.Printf("Server P2P: Successfully re-advertised on DHT (Rendezvous: %s)", ServerRendezvousString)
				}
			}
		}
	}()
}

func (pm *ServerP2PManager) advertiseWithRetry(ctx context.Context, rendezvous string) {
	maxRetries := 5 // Fewer retries, rely on periodic ticker for long term
	baseDelay := 30 * time.Second
	maxDelay := 3 * time.Minute

	for attempt := 0; attempt < maxRetries; attempt++ {
		if ctx.Err() != nil {
			pm.logger.Printf("Server P2P: Advertisement context cancelled before attempt for '%s'.", rendezvous)
			return
		}

		routingTableSize := pm.kademliaDHT.RoutingTable().Size()
		pm.logger.Printf("Server P2P: Advertisement attempt %d for '%s' - DHT routing table has %d peers",
			attempt+1, rendezvous, routingTableSize)

		if attempt == 0 && routingTableSize < 3 { // Wait if DHT is very sparse initially
			pm.logger.Printf("Server P2P: DHT routing table very small for '%s', waiting before first advertise...", rendezvous)
			select {
			case <-time.After(30 * time.Second):
			case <-ctx.Done():
				pm.logger.Printf("Server P2P: Advertisement context cancelled during initial wait for '%s'.", rendezvous)
				return
			}
		}

		advCtx, cancelAdv := context.WithTimeout(ctx, 1*time.Minute)
		_, err := pm.routingDiscovery.Advertise(advCtx, rendezvous)
		cancelAdv()

		if err == nil {
			pm.logger.Printf("Server P2P: Successfully advertised '%s' on attempt %d", rendezvous, attempt+1)
			return // Success, initial advertisement done, periodic ticker will handle re-advertisements
		}
		pm.logger.Printf("Server P2P: Advertisement attempt %d for '%s' failed: %v", attempt+1, rendezvous, err)

		if attempt < maxRetries-1 {
			delay := baseDelay * time.Duration(attempt+1)
			if delay > maxDelay {
				delay = maxDelay
			}
			pm.logger.Printf("Server P2P: Retrying advertisement for '%s' in %v...", rendezvous, delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				pm.logger.Printf("Server P2P: Advertisement retry for '%s' cancelled", rendezvous)
				return
			}
		}
	}
	pm.logger.Printf("Server P2P: Initial advertisement attempts for '%s' failed. Periodic ticker will continue trying.", rendezvous)
}

func (pm *ServerP2PManager) monitorAddressAndNATChanges(ctx context.Context) {
	addrSub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated))
	if err != nil {
		pm.logger.Printf("Server P2P ERROR: Failed to subscribe to address update events: %v", err)
		return
	}
	defer addrSub.Close()

	reachabilitySub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		pm.logger.Printf("Server P2P ERROR: Failed to subscribe to reachability events: %v", err)
	} else {
		defer reachabilitySub.Close()
	}

	pm.logger.Printf("Server P2P: Monitoring address and NAT changes. Initial Listen Addresses: %v", pm.host.Addrs())

	for {
		select {
		case <-ctx.Done():
			pm.logger.Println("Server P2P: Address/NAT monitor stopping.")
			return
		case ev, ok := <-addrSub.Out():
			if !ok {
				pm.logger.Println("Server P2P: Address subscription closed.")
				addrSub = nil // Avoid using closed channel
				// If reachabilitySub is also closed, break
				if reachabilitySub == nil {
					return
				}
				continue
			}
			addressEvent := ev.(event.EvtLocalAddressesUpdated)
			pm.logger.Printf("Server P2P: Host addresses updated.")
			for _, currentAddr := range addressEvent.Current {
				pm.logger.Printf("Server P2P:  -> Current: %s (Action: %s)", currentAddr.Address, currentAddr.Action)
			}
			for _, removedAddr := range addressEvent.Removed {
				pm.logger.Printf("Server P2P:  -> Removed: %s", removedAddr.Address)
			}
		case ev, ok := <-reachabilitySub.Out():
			if !ok {
				pm.logger.Println("Server P2P: Reachability subscription closed.")
				reachabilitySub = nil // Avoid using closed channel
				if addrSub == nil {
					return
				}
				continue
			}
			reachabilityEvent := ev.(event.EvtLocalReachabilityChanged)
			pm.logger.Printf("Server P2P: Reachability status changed to: %s", reachabilityEvent.Reachability.String())
			if reachabilityEvent.Reachability == network.ReachabilityPublic {
				pm.logger.Println("Server P2P: Yay! Reachability is PUBLIC. Direct connections should be possible.")
			} else if reachabilityEvent.Reachability == network.ReachabilityPrivate {
				pm.logger.Println("Server P2P: Reachability is PRIVATE. Direct connections from internet may require manual port forwarding or rely on relays/hole-punching.")
			}
		}
	}
}

func (pm *ServerP2PManager) Close() error {
	pm.logger.Println("Server P2P: Shutting down P2P Manager...")
	pm.p2pCancel() // Signal all goroutines using p2pCtx to stop

	pm.logger.Println("Server P2P: Waiting for background goroutines to complete...")
	pm.shutdownWg.Wait() // Wait for goroutines like advertising, monitoring to finish

	if pm.mdnsService != nil {
		pm.logger.Println("Server P2P: Closing mDNS service...")
		if err := pm.mdnsService.Close(); err != nil {
			pm.logger.Printf("Server P2P: Error closing mDNS service: %v", err)
		}
	}
	// DHT shuts down when the host closes (uses same context). So just closing host is sufficient.

	var Rerr error
	if pm.host != nil {
		pm.logger.Println("Server P2P: Closing libp2p host...")
		if err := pm.host.Close(); err != nil {
			pm.logger.Printf("Server P2P: Error closing libp2p host: %v", err)
			Rerr = err
		}
	}
	pm.logger.Println("Server P2P: P2P Manager shut down complete.")
	return Rerr
}

// handleIncomingLogStream: Ensure robust error handling and response sending
func handleIncomingLogStream(stream network.Stream, logger *log.Logger, store *ServerLogStore, encryptionKeyHex string) {
	remotePeer := stream.Conn().RemotePeer()
	logger.Printf("Server P2P: Received new log stream from: %s", remotePeer.String())

	// Set deadlines for the stream operations
	// Use a context for the overall handling of this stream
	streamCtx, cancelStream := context.WithTimeout(context.Background(), 2*time.Minute) // Max 2 mins to process a batch
	defer cancelStream()
	defer func() {
		stream.Reset() // More forceful than Close, indicates abnormal termination or completion
		logger.Printf("Server P2P: Reset log stream from: %s", remotePeer.String())
	}()

	var request p2p.LogBatchRequest
	errRead := p2p.ReadMessageWithTimeout(stream, &request, 60*time.Second) // Read with timeout
	if errRead != nil {
		if errRead == io.EOF {
			logger.Printf("Server P2P: Stream from %s closed by remote (EOF) while reading request.", remotePeer)
		} else if netErr, ok := errRead.(net.Error); ok && netErr.Timeout() {
			logger.Printf("Server P2P: Timeout reading LogBatchRequest from %s: %v", remotePeer, errRead)
		} else {
			logger.Printf("Server P2P: Error reading LogBatchRequest from %s: %v", remotePeer, errRead)
		}
		// Cannot reliably send response if read failed badly.
		return
	}
	logger.Printf("Server P2P: Received LogBatchRequest (ID: %s) from ClientAppID: %s, PayloadSize: %d bytes",
		request.Header.RequestID, request.AppClientID, len(request.EncryptedLogPayload))

	// Decryption
	decryptedJSON, err := pkgCrypto.Decrypt(request.EncryptedLogPayload, encryptionKeyHex)
	if err != nil {
		logger.Printf("Server P2P: Failed to decrypt payload from %s (ClientAppID: %s, RequestID: %s): %v",
			remotePeer, request.AppClientID, request.Header.RequestID, err)
		_ = p2p.SendErrorResponse(stream, request.Header.RequestID, "DECRYPT_FAIL", "Decryption failed on server.", false)
		return
	}

	// Unmarshal
	var logEvents []types.LogEvent
	if err := json.Unmarshal(decryptedJSON, &logEvents); err != nil {
		logger.Printf("Server P2P: Failed to unmarshal LogEvents JSON from %s (ClientAppID: %s, RequestID: %s): %v",
			remotePeer, request.AppClientID, request.Header.RequestID, err)
		_ = p2p.SendErrorResponse(stream, request.Header.RequestID, "UNMARSHAL_FAIL", "JSON unmarshal failed on server.", false)
		return
	}
	logger.Printf("Server P2P: Unmarshaled %d log events from ClientAppID: %s (RequestID: %s)",
		len(logEvents), request.AppClientID, request.Header.RequestID)

	if streamCtx.Err() != nil { // Check if overall stream context timed out
		logger.Printf("Server P2P: Stream context timed out before storing logs from %s (RequestID: %s)", remotePeer, request.Header.RequestID)
		return
	}

	// Store events
	processedCount, err := store.AddReceivedLogEvents(logEvents, time.Now().UTC())
	if err != nil {
		logger.Printf("Server P2P: Failed to store log events from %s (ClientAppID: %s, RequestID: %s): %v",
			remotePeer, request.AppClientID, request.Header.RequestID, err)
		// Determine if retryable (e.g. temporary DB issue vs. bad data)
		// For now, assume not retryable by client for simplicity, server needs fixing.
		_ = p2p.SendErrorResponse(stream, request.Header.RequestID, "DB_ERROR", fmt.Sprintf("Server DB error: %s", err.Error()), false)
		return
	}

	// Send success response
	errWrite := p2p.SendLogBatchResponse(stream, request.Header.RequestID, "success",
		fmt.Sprintf("Successfully processed %d log events.", processedCount),
		processedCount, len(logEvents)-processedCount)
	if errWrite != nil {
		if netErr, ok := errWrite.(net.Error); ok && netErr.Timeout() {
			logger.Printf("Server P2P: Timeout writing success response to %s (RequestID: %s): %v", remotePeer, request.Header.RequestID, errWrite)
		} else {
			logger.Printf("Server P2P: Failed to send success response to %s (RequestID: %s): %v", remotePeer, request.Header.RequestID, errWrite)
		}
	} else {
		logger.Printf("Server P2P: Sent success response to %s for %d events (ClientAppID: %s, RequestID: %s).",
			remotePeer, processedCount, request.AppClientID, request.Header.RequestID)
	}
}

// runServerP2PManager controls the ServerP2PManager lifecycle
func runServerP2PManager(
	p2pManager *ServerP2PManager,
	internalQuit <-chan struct{}, // Main application quit signal
	wg *sync.WaitGroup, // Main application WaitGroup
	logger *log.Logger,
) {
	defer wg.Done()
	p2pManager.logger.Println("Server P2P Manager controlling goroutine starting.")

	p2pManager.StartBootstrapAndDiscovery() // This now uses p2pManager.p2pCtx

	select {
	case <-internalQuit: // Signal from main application to shut down
		p2pManager.logger.Println("Server P2P Manager: Received internal quit signal. Initiating shutdown...")
	case <-p2pManager.p2pCtx.Done(): // If P2P manager's internal context is cancelled (e.g., unrecoverable error)
		p2pManager.logger.Println("Server P2P Manager: Internal P2P context done. Initiating shutdown...")
	}

	// Calling Close will cancel p2pCtx (if not already) and wait for goroutines.
	if err := p2pManager.Close(); err != nil {
		p2pManager.logger.Printf("Server P2P Manager: Error during Close: %v", err)
	}
	p2pManager.logger.Println("Server P2P Manager controlling goroutine finished.")
}
