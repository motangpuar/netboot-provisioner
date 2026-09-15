package main

import "context"
import "log"
import "flag"
import "net"
import "fmt"
import "time"
import "os"
//import "github.com/insomniacslk/dhcp/dhcpv4"
//import "github.com/insomniacslk/dhcp/dhcpv4/client4"
import "github.com/insomniacslk/dhcp/dhcpv4/nclient4"


func main() {

	ifaceName := flag.String("i","dummy-client","Interface for DHCP Client")
	flag.Parse()
	log.Printf("Staring DHCP Client at %s", *ifaceName)

	_,err := net.InterfaceByName(*ifaceName)
	if err != nil {
		log.Fatalf("Interface Error: %v", err)
	}

	// discover, err := dhcpv4.NewDiscovery(iface.HardwareAddr)
	// if err != nil {
	// 	log.Fatalf("Payload Construcition error: %v", err)
	// } else {
	// 	log.Printf("Discover at MAC: %s", discover)
	// }

	//client := client4.NewClient()
	client,err := nclient4.New(
		*ifaceName,
		nclient4.WithTimeout(10*time.Second),
		nclient4.WithRetry(3),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to create DHCP client %v", err)
		os.Exit(1)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lease,err := client.Request(ctx)
	if err != nil {
		log.Fatalf("DHCP request failed: %v", err)
	}
	if lease == nil || lease.ACK == nil {
		log.Fatal("DHCP request returned an empty lease")
	}

	log.Println("Lease Summary: ")
	log.Println(lease.ACK.Summary())

}


