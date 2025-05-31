// GuiKeyStandaloneGo/generator/templates/server_template/p2p_manager.go
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
	"github.com/libp2p/go-libp2p/core/peer"
	coreRouting "github.com/libp2p/go-libp2p/core/routing"
	routd "github.com/libp2p/go-libp2p/p2p/discovery/routing"

	// AutoNAT import not explicitly needed if using libp2p.EnableNATService() option
	"github.com/multiformats/go-multiaddr"
)

const ServerRendezvousString = "guikey-standalone-logserver/v1.0.0" // Versioned rendezvous

type ServerP2PManager struct {
	host               host.Host
	kademliaDHT        *dht.IpfsDHT
	routingDiscovery   *routd.RoutingDiscovery
	logger             *log.Logger
	serverLogStore     *ServerLogStore
	encryptionKeyHex   string
	bootstrapAddrInfos []peer.AddrInfo
}

func NewServerP2PManager(
	logger *log.Logger,
	listenAddrStr string,
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

	var opts []libp2p.Option
	opts = append(opts, libp2p.Identity(privKey))

	listenMA, err := multiaddr.NewMultiaddr(listenAddrStr)
	if err != nil {
		return nil, fmt.Errorf("server_p2p: invalid listen multiaddress '%s': %w", listenAddrStr, err)
	}
	opts = append(opts, libp2p.ListenAddrs(listenMA))

	opts = append(opts, libp2p.EnableRelay())
	opts = append(opts, libp2p.EnableHolePunching())
	opts = append(opts, libp2p.EnableNATService()) // Fixed: EnableAutoNAT() -> EnableNATService()
	opts = append(opts, libp2p.NATPortMap())

	var kadDHT *dht.IpfsDHT
	opts = append(opts,
		libp2p.Routing(func(h host.Host) (coreRouting.PeerRouting, error) {
			var dhtErr error
			dhtOpts := []dht.Option{dht.Mode(dht.ModeServer)}
			if len(bootAddrsInfo) > 0 {
				dhtOpts = append(dhtOpts, dht.BootstrapPeers(bootAddrsInfo...))
			}
			kadDHT, dhtErr = dht.New(context.Background(), h, dhtOpts...)
			return kadDHT, dhtErr
		}),
	)

	p2pHost, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("server_p2p: failed to create libp2p host: %w", err)
	}
	logger.Printf("Server P2P: Host created. ID: %s", p2pHost.ID().String())

	if kadDHT == nil {
		p2pHost.Close()
		return nil, fmt.Errorf("server_p2p: Kademlia DHT was not initialized")
	}
	logger.Println("Server P2P: Kademlia DHT initialized.")

	routingDiscovery := routd.NewRoutingDiscovery(kadDHT)

	p2pHost.SetStreamHandler(p2p.ProtocolID, func(stream network.Stream) {
		handleIncomingLogStream(stream, logger, serverLogStore, appEncryptionKeyHex)
	})
	logger.Printf("Server P2P: Stream handler set for protocol ID: %s", p2p.ProtocolID)

	return &ServerP2PManager{
		host:               p2pHost,
		kademliaDHT:        kadDHT,
		routingDiscovery:   routingDiscovery,
		logger:             logger,
		serverLogStore:     serverLogStore,
		encryptionKeyHex:   appEncryptionKeyHex,
		bootstrapAddrInfos: bootAddrsInfo,
	}, nil
}

func (pm *ServerP2PManager) StartBootstrapAndDiscovery(ctx context.Context) {
	pm.logger.Println("Server P2P: Starting bootstrap, discovery, and advertising processes...")
	if len(pm.bootstrapAddrInfos) > 0 {
		pm.logger.Printf("Server P2P: Connecting to %d bootstrap peers...", len(pm.bootstrapAddrInfos))
		var wg sync.WaitGroup
		for _, pi := range pm.bootstrapAddrInfos {
			if pm.host.Network().Connectedness(pi.ID) == network.Connected {
				continue
			}
			wg.Add(1)
			go func(peerInfo peer.AddrInfo) {
				defer wg.Done()
				connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
				if err := pm.host.Connect(connectCtx, peerInfo); err != nil {
					// pm.logger.Printf("Server P2P: Failed to connect to bootstrap peer %s: %v", peerInfo.ID, err)
				} else {
					pm.logger.Printf("Server P2P: Successfully connected to bootstrap peer: %s", peerInfo.ID.String())
				}
			}(pi)
		}
	} else {
		pm.logger.Println("Server P2P: No bootstrap peers configured for server.")
	}

	pm.logger.Println("Server P2P: Bootstrapping Kademlia DHT...")
	go func() { // Run bootstrap in a goroutine
		// Give initial connections to bootstrap nodes a moment.
		select {
		case <-time.After(10 * time.Second):
		case <-ctx.Done():
			return
		}

		if err := pm.kademliaDHT.Bootstrap(ctx); err != nil {
			pm.logger.Printf("Server P2P: Kademlia DHT bootstrap process failed: %v", err)
		} else {
			pm.logger.Println("Server P2P: Kademlia DHT bootstrap process completed/initiated.")
			pm.startAdvertising(ctx) // Start advertising after bootstrap
		}
	}()

	go pm.monitorAddressAndNATChanges(ctx)
}

func (pm *ServerP2PManager) startAdvertising(ctx context.Context) {
	if pm.routingDiscovery == nil {
		pm.logger.Println("Server P2P: RoutingDiscovery not initialized, cannot advertise.")
		return
	}
	pm.logger.Printf("Server P2P: Starting to advertise self on DHT with rendezvous string: %s", ServerRendezvousString)

	// Initial advertisement - Using simple Advertise method without options
	_, err := pm.routingDiscovery.Advertise(ctx, ServerRendezvousString)
	if err != nil {
		pm.logger.Printf("Server P2P: Error on initial advertisement: %v", err)
	} else {
		pm.logger.Printf("Server P2P: Successfully performed initial advertisement for %s", ServerRendezvousString)
	}

	advertiseTicker := time.NewTicker(5 * time.Minute)
	defer advertiseTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			pm.logger.Println("Server P2P: Stopping advertisement due to context cancellation.")
			return
		case <-advertiseTicker.C:
			pm.logger.Printf("Server P2P: Re-advertising self on DHT: %s", ServerRendezvousString)
			// Re-advertise without TTL options
			_, errD := pm.routingDiscovery.Advertise(ctx, ServerRendezvousString)
			if errD != nil {
				pm.logger.Printf("Server P2P: Error re-advertising self on DHT: %v", errD)
			}
		}
	}
}

func (pm *ServerP2PManager) monitorAddressAndNATChanges(ctx context.Context) {
	addrSub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated))
	if err != nil {
		pm.logger.Printf("Server P2P: Failed to subscribe to address update events: %v", err)
		return
	}
	defer addrSub.Close()

	reachabilitySub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		pm.logger.Printf("Server P2P: Failed to subscribe to reachability events: %v", err)
	} else {
		defer reachabilitySub.Close()
	}

	pm.logger.Printf("Server P2P: Initial Listen Addresses: %v", pm.host.Addrs())

	for {
		select {
		case <-ctx.Done():
			pm.logger.Println("Server P2P: Address/NAT monitor stopping.")
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
			pm.logger.Printf("Server P2P: Host addresses updated. Current: %v, Diffs: %+v", addressEvent.Current, addressEvent.Diffs)
		case ev, ok := <-reachabilitySub.Out():
			if !ok {
				reachabilitySub = nil
				if addrSub == nil {
					return
				}
				continue
			}
			reachabilityEvent := ev.(event.EvtLocalReachabilityChanged)
			pm.logger.Printf("Server P2P: Reachability status changed to: %s", reachabilityEvent.Reachability.String())
		}
	}
}

func handleIncomingLogStream(stream network.Stream, logger *log.Logger, store *ServerLogStore, encryptionKeyHex string) {
	remotePeer := stream.Conn().RemotePeer()
	logger.Printf("Server P2P: Received new log stream from: %s", remotePeer.String())
	defer func() {
		stream.Reset()
		logger.Printf("Server P2P: Closed log stream from: %s", remotePeer.String())
	}()

	_ = stream.SetReadDeadline(time.Now().Add(60 * time.Second))
	_ = stream.SetWriteDeadline(time.Now().Add(30 * time.Second))

	var request p2p.LogBatchRequest
	if err := p2p.ReadMessage(stream, &request); err != nil {
		if err == io.EOF {
			logger.Printf("Server P2P: Stream from %s closed by remote before full request (EOF).", remotePeer)
		} else {
			logger.Printf("Server P2P: Error reading LogBatchRequest from %s: %v", remotePeer, err)
		}
		return
	}
	logger.Printf("Server P2P: Received LogBatchRequest from ClientAppID: %s, PayloadSize: %d bytes", request.AppClientID, len(request.EncryptedLogPayload))

	decryptedJSON, err := pkgCrypto.Decrypt(request.EncryptedLogPayload, encryptionKeyHex)
	if err != nil {
		logger.Printf("Server P2P: Failed to decrypt payload from %s (ClientAppID: %s): %v", remotePeer, request.AppClientID, err)
		resp := p2p.LogBatchResponse{Status: "error", Message: "Decryption failed on server.", ServerTimestamp: time.Now().Unix()}
		if wErr := p2p.WriteMessage(stream, &resp); wErr != nil {
			logger.Printf("Server P2P: Failed to send error (decryption) response to %s: %v", remotePeer, wErr)
		}
		return
	}

	var logEvents []types.LogEvent
	if err := json.Unmarshal(decryptedJSON, &logEvents); err != nil {
		logger.Printf("Server P2P: Failed to unmarshal LogEvents JSON from %s (ClientAppID: %s): %v", remotePeer, request.AppClientID, err)
		resp := p2p.LogBatchResponse{Status: "error", Message: "JSON unmarshal failed on server.", ServerTimestamp: time.Now().Unix()}
		if wErr := p2p.WriteMessage(stream, &resp); wErr != nil {
			logger.Printf("Server P2P: Failed to send error (unmarshal) response to %s: %v", remotePeer, wErr)
		}
		return
	}
	logger.Printf("Server P2P: Unmarshaled %d log events from ClientAppID: %s", len(logEvents), request.AppClientID)

	processedCount, err := store.AddReceivedLogEvents(logEvents, time.Now().UTC())
	if err != nil {
		logger.Printf("Server P2P: Failed to store log events from %s (ClientAppID: %s): %v", remotePeer, request.AppClientID, err)
		resp := p2p.LogBatchResponse{Status: "error", Message: fmt.Sprintf("Server DB error: %s", err.Error()), EventsProcessed: processedCount, ServerTimestamp: time.Now().Unix()}
		if wErr := p2p.WriteMessage(stream, &resp); wErr != nil {
			logger.Printf("Server P2P: Failed to send error (DB) response to %s: %v", remotePeer, wErr)
		}
		return
	}

	successResp := p2p.LogBatchResponse{
		Status:          "success",
		Message:         fmt.Sprintf("Successfully processed %d log events.", processedCount),
		EventsProcessed: processedCount,
		ServerTimestamp: time.Now().Unix(),
	}
	if err := p2p.WriteMessage(stream, &successResp); err != nil {
		logger.Printf("Server P2P: Failed to send success response to %s: %v", remotePeer, err)
	} else {
		logger.Printf("Server P2P: Sent success response to %s for %d events (ClientAppID: %s).", remotePeer, processedCount, request.AppClientID)
	}
}

func (pm *ServerP2PManager) Close() error {
	if pm.kademliaDHT != nil {
		if err := pm.kademliaDHT.Close(); err != nil {
			pm.logger.Printf("Server P2P: Error closing Kademlia DHT: %v", err)
		}
	}
	if pm.host != nil {
		pm.logger.Println("Server P2P: Closing libp2p host...")
		return pm.host.Close()
	}
	return nil
}

func runServerP2PManager(
	p2pManager *ServerP2PManager,
	internalQuit <-chan struct{},
	wg *sync.WaitGroup,
	logger *log.Logger,
) {
	defer wg.Done()
	p2pManager.logger.Println("Server P2P Manager goroutine starting.")

	p2pCtx, cancelP2pCtx := context.WithCancel(context.Background())
	defer cancelP2pCtx()

	p2pManager.StartBootstrapAndDiscovery(p2pCtx)

	<-internalQuit
	p2pManager.logger.Println("Server P2P Manager goroutine stopping...")
}
