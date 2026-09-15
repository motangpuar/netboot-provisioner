package filedata

import "sync"
import "log"
import "os"
import "bufio"
import "strings"
import "fmt"
import "github.com/motangpuar/o2-ims-worker/internal/inventory"
import "github.com/motangpuar/o2-ims-worker/internal/kubernetes"
import "github.com/motangpuar/o2-ims-worker/internal/ansible"

//
// This package will manage staticlly defined users and introduce them to dhcp
// and tftp package
// Milestone:
// 1. Struct based clients
// 2. CSV based clients
// 3. DB based clients -> dbdata
// 4. Pool based broadcast, different subnets for different clusters
//
//
// Main Interface
// This interface extend beyond this package
//
type Client interface { 
	OfferIP() string
	OfferGateway() string
	MACAddress() string
	BootFileUrl() string
	OSType() string
	ToMap() map[string]any
	GetTemplate() string
	GetCluster() string
	GetMachines() inventory.MachineObject
}

type dhcpClients struct {
	userID string
	offerIP string
	offerGateway string
	macAddress string
	bootFileUrl string
	osType string
	machineObject inventory.MachineObject
}

// Make it into map so it will always be O(1) upon reading from DHCP
type ptrClients struct {
	//Clients []*dhcpClients
	Clients map[string]Client
}

func AddItem(ip, mac, osType string) {

	newClients := dhcpClients{
		offerIP: ip,
		macAddress: mac,
		bootFileUrl: "pxelinux.0",
		osType: osType,
	}

	clients := Gather().Clients
	clients[mac] = &newClients

	for _,c := range clients {
		log.Printf("[Current Struct] %s", c)
	}

	log.Printf("[Current Struct] %s", newClients)
	log.Printf("[Add File] Total Section %d", len(clients))

}

func AddItemToFile(ip, mac, osType, secret string) {
	
	clients := Gather().Clients

	if clients[mac] != nil {
		log.Printf("[FILE] Entry Exist: %s", mac)
		return 
	}

	for _,c := range clients {
		if c.OfferIP() == ip {
			log.Printf("[FILE] IP Exist: %s", c.MACAddress())
			return 
		}
	}

	file, err := os.OpenFile("inputs/clients.csv", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		log.Printf("[FILE] Error Opening File: %v", err)
		return 
	}

	defer file.Close()

	writer := bufio.NewWriter(file)
	_, err = fmt.Fprintf(writer, "%s,%s,pxelinux.0,%s,,,,\n", ip, mac, osType )
	if err != nil {
		log.Printf("[FILE] Error: %v", err)
		return
	}

	err = writer.Flush()
	if err != nil {
		log.Printf("[FILE] Error flush: %v", err)
	}

	Populate(secret)

}

func Populate(secret string) {
	log.Println("[Static DB Realm]")

	newClients1 := dhcpClients{
		offerIP: "192.168.99.200",
		macAddress: "e2:37:36:e8:12:b7",
		bootFileUrl: "pxelinux.0",
		osType: "centos",
	}

	newClients2 := dhcpClients{
		offerIP: "192.168.99.201",
		macAddress: "e2:37:26:f8:63:b5",
		bootFileUrl: "second-user-pxelinux.0",
		osType: "centos",
	}

	clients := []*dhcpClients{
		&newClients1,
		&newClients2,
	}

	// Process Files
	data, err := os.Open("inputs/clients.csv")
	if err != nil {
		log.Printf("Failed to open file %v", err)
	}

	// Run last 
	defer data.Close()

	scanner := bufio.NewScanner(data)
	if scanner.Err() != nil {
		log.Fatalf("Failed to scan CSV file: %s", err)
	}

	// Skip first line by start an empty Scan() function from scanner
	if scanner.Scan() {}

	// Populate Template struct with empty
	inventory.Init()
	m := make(map[string]Client, len(clients))

	clustersMap := make(map[string][]struct{
		Node string
		Role string
		OS string
		IP string
	})

	for scanner.Scan() {
		cLine := scanner.Text()

		//log.Printf("[BUFIO] %s", cLine)
		read_lines := strings.Split(cLine, ",")
		currIP := read_lines[0]
		currMAC := read_lines[1]
		currFileUrl := read_lines[2]
		currOS := read_lines[3]
		currCluster := read_lines[4]
		currTemplate := read_lines[5]
		currRole := read_lines[6]
		currGW := read_lines[7]

		if currCluster == "" {
			currCluster = "unclaimed"
		}

		// Inventory Object
		invObj := inventory.MachineObject{}
		machineObj := invObj.Generate(
			currIP,
			currMAC,
			currOS,
			currCluster,
			currRole,
			currTemplate)

		currClient := &dhcpClients{
				offerIP: currIP,
				offerGateway: currGW,
				macAddress: currMAC,
				bootFileUrl: currFileUrl,
				osType: currOS,
				machineObject: *machineObj,
			}
		clients = append(clients, currClient)
		m[currMAC] = currClient

		clustersMap[currCluster] = append(clustersMap[currCluster],
			struct {
				Node string
				Role string
				OS string
				IP string
			}{
				Node: currMAC,
				Role: currRole,
				OS: currOS,
				IP: currIP,
		})

	}

	//iterate over machine to find cluster based on master
	// Populate Cluster Object
	// Start this 
	go func() {
		var wg sync.WaitGroup
		for k,n := range(clustersMap) {
			log.Printf("[Struct] ClusterMAP %s", k)
			log.Printf("[Struct] ClusterNode %s", n)
			clusterNodes := n
			kubeclient.InitCluster(k, &clusterNodes)
			for _,d := range(n) {
				wg.Add(1)

				// Run the validate cluster on the fly concurrently
				// freeze current value so other process not affecting 
				// current values
				go func(nodeData struct{
					Node string
					Role string
					OS string
					IP string
				}){
					defer wg.Done()
					ansible_worker.FetchToken(nodeData.IP, nodeData.OS, nodeData.Node)
				}(d)
			}
		}
		wg.Wait()
		// This is placeholder for event logs
		log.Println("[Cycle Validation] SUCCESS: All background tasks finished properly!")
	}()


	log.Printf("[Struct] Total Section %d", len(clients))
	// Intialize pointer of Clients
	activePtr = &ptrClients{
		Clients: m,
	}
}

var activePtr *ptrClients

// Return Pointer when asked
func Gather() *ptrClients {
	return activePtr
}

// Client Specific Values
func (d *dhcpClients) OfferIP() string { return d.offerIP }
func (d *dhcpClients) OfferGateway() string { return d.offerGateway }
func (d *dhcpClients) MACAddress() string { return d.macAddress }
func (d *dhcpClients) BootFileUrl() string { return d.bootFileUrl }
func (d *dhcpClients) OSType() string { return d.osType }

// Extend to other package
func (d *dhcpClients) GetTemplate() string { return d.machineObject.Template }
func (d *dhcpClients) GetCluster() string { return d.machineObject.Cluster }
func (d *dhcpClients) GetMachines() inventory.MachineObject { return d.machineObject }

// Bulk Interface
func (d *dhcpClients) DHCPClient() *dhcpClients { return d }
func (d *dhcpClients) ToMap() map[string]any {
	return map[string]any{
		"ip": d.offerIP,
		"mac": d.macAddress,
		"bootfile": d.bootFileUrl,
		"ostype": d.osType,
	}
}
