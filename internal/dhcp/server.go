package dhcp

import "log"
import "net"
import "github.com/insomniacslk/dhcp/dhcpv4"
import "github.com/insomniacslk/dhcp/dhcpv4/server4"
import "github.com/motangpuar/o2-ims-worker/internal/db"
import "strings"
import "time"

// Reacive pointer that have exactly these methods
type Reader interface {
	BindAddr() string
	BindPort() int
	Enabled() bool
	Mode() string
	BindInterface() string
	TFTPIP() string
	TFTPPort() int
	NextServe() string
	BootFilePath() string
}

// Learn more about this shit
type Engine struct {
	cfg Reader
}

// Process the ability of the pointer
func NewEngine(r Reader) *Engine {
	return &Engine{
		cfg: r,
	}
}

// isPXEClient checks if the client is requesting PXE boot
func isPXEClient(m *dhcpv4.DHCPv4) bool {

    // Check for PXE client identifier
    vendorClass := m.Options.Get(dhcpv4.OptionClassIdentifier)
    if vendorClass != nil && (string(vendorClass) == "PXEClient" || string(vendorClass) == "PXEClient:Arch:00000:UNDI:002001") {
        return true
    }

    // Check for Option 93 (Client System Architecture)
    archType := m.Options.Get(dhcpv4.OptionClientSystemArchitectureType)
    if archType != nil {
        return true
    }

    // Check for Option 94 (UUID/GUID)
    clientUUID := m.Options.Get(dhcpv4.OptionClientNetworkInterfaceIdentifier)
    if clientUUID != nil {
        return true
    }

    return false

}

func isLegacyPXEClient(m *dhcpv4.DHCPv4) bool {
    // Check vendor class identifier
    vendorClass := m.Options.Get(dhcpv4.OptionClassIdentifier)
    if vendorClass != nil {
        vcString := string(vendorClass)
        // These patterns often indicate legacy PXE clients
        legacyPatterns := []string{
            "PXEClient:Arch:00000", // Very old PXE
            "PXEClient:Arch:00006", // IA32 BIOS PXE clients (most legacy clients)
            "PXEClient:Arch:00007", // x86-64 BIOS PXE clients (some older x64 systems)
        }
        
        for _, pattern := range legacyPatterns {
            if strings.Contains(vcString, pattern) {
                return true
            }
        }
    }
    
    // If client has no IP and requests broadcast, likely legacy
    if m.ClientIPAddr.IsUnspecified() && m.IsBroadcast() {
        return true
    }
    
    // Client system architecture type can also indicate legacy clients
    archType := m.Options.Get(dhcpv4.OptionClientSystemArchitectureType)
    if archType != nil && len(archType) >= 2 {
        // Check if architecture type is legacy BIOS PC (type 0)
        if archType[0] == 0 && archType[1] == 0 {
            return true
        }
    }
    
    return false
}

func (e *Engine) handler(conn net.PacketConn, peer net.Addr, m *dhcpv4.DHCPv4) {

	log.Printf("----------------------------------------")
	reqMAC := m.ClientHWAddr.String()

    log.Printf("Received DHCP request from %s, type: %s", reqMAC, m.MessageType())
    isLegacyPXE := isLegacyPXEClient(m)
    if isLegacyPXE {
        log.Printf("DHCP: Detected legacy PXE client: %s", reqMAC)
    }

    reply, err := dhcpv4.NewReplyFromRequest(m)
    if err != nil {
        log.Printf("NewReplyFromRequest failed: %v", err)
        return
    }

	//
	// Check condition should be here
	// Based on MAC address
	// If MAC is listed on file or db
	// offerIP Accordingly?
	//
	
	fd := filedata.Gather()
	var offerIP string
	var bootFileName string
	var offerGateway string
	
	// O(1) Lookup based on DHCP RequestMAC
	isClient := fd.Clients[reqMAC]
	log.Println(isClient)

	if isClient != nil {
		cMACAddress := isClient.MACAddress()
		cOfferIP := isClient.OfferIP()
		cBootFileName := isClient.BootFileUrl()

		// POpulate
		offerIP = cOfferIP
		offerGateway = isClient.OfferGateway()
		bootFileName = cBootFileName

		log.Printf("IP: %s, Mac: %s, File: %s", cOfferIP, cMACAddress, bootFileName)
	} else {
		log.Printf("Client %s Not Found on DB", reqMAC)
		return
	}

    // LegacyPXE 
    if isLegacyPXE {
        reply.SetBroadcast()
        log.Printf("DHCP: Using broadcast response for legacy PXE client")
    } else {
        // Use unicast for modern clients
        reply.SetUnicast()
        log.Printf("DHCP: Using unicast response for client")
    }

    serverIP := net.ParseIP(offerGateway)

    log.Printf("Offering IP %s to client %s (Gateway: %s)", offerIP, m.ClientHWAddr.String(), offerGateway)
	
    reply.YourIPAddr = net.ParseIP(offerIP) // Temporary IP
    reply.ServerIPAddr = serverIP
    reply.GatewayIPAddr = net.ParseIP(offerGateway)

	bootServer := serverIP
    reply.ServerHostName = offerGateway
    reply.BootFileName = bootFileName
    reply.UpdateOption(dhcpv4.OptServerIdentifier(serverIP))
    
    // Network config
    reply.UpdateOption(dhcpv4.OptSubnetMask(net.IPMask(net.IP{255, 255, 255, 0})))
    reply.UpdateOption(dhcpv4.OptRouter(net.ParseIP(offerGateway)))
    reply.UpdateOption(dhcpv4.OptDNS(net.IP{8, 8, 8, 8}))
    
    // PXE options
    reply.UpdateOption(dhcpv4.OptTFTPServerName(bootServer.String()))
    reply.UpdateOption(dhcpv4.OptBootFileName(bootFileName))
    reply.UpdateOption(dhcpv4.OptIPAddressLeaseTime(time.Hour * 12))
    reply.UpdateOption(dhcpv4.OptIPAddressLeaseTime(time.Hour * 12))
    
    // Set appropriate response type
    switch mt := m.MessageType(); mt {
    case dhcpv4.MessageTypeDiscover:
        log.Printf("DHCP Discover from %s will Offer", m.ClientHWAddr)
        reply.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeOffer))
    case dhcpv4.MessageTypeRequest:
        log.Printf("DHCP Request from %s will ACK", m.ClientHWAddr)
        reply.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeAck))
    default:
        log.Printf("Unhandled Message type: %v", mt)
        return
    }
    // Log the reply we're sending
    //log.Printf("Sending reply: %s", reply.Summary())
    // Send the response
    if _, err := conn.WriteTo(reply.ToBytes(), peer); err != nil {
        log.Printf("Cannot reply to client: %v", err)
        return
    }
}

func (e *Engine) Start() {
	log.Println("[DHCP Realm]--------------<*>")

	bindAddr := e.cfg.BindAddr()
	bindPort := e.cfg.BindPort()
	bindInterface := e.cfg.BindInterface()

	log.Println(bindAddr)
	log.Println(bindPort)
	log.Println(e.cfg.TFTPIP())

	lAddr := &net.UDPAddr{
		IP: net.ParseIP(bindAddr),
		Port: bindPort,
	}

	log.Println("DHCP Addr: ",lAddr)
	log.Println("DHCP Interface: ",bindInterface)
	log.Println("DHCP Port: ",bindPort)

	server, err := server4.NewServer(bindInterface,lAddr, e.handler)
	if err != nil {
		log.Fatalf("Failed to create DHCP server: %v", err)
	}

	if err := server.Serve(); err != nil {
		log.Fatalf("DHCP server error: %v", err)
	}
}

