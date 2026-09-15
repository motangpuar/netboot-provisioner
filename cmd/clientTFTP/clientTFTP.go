package main

import "fmt"
import "os"
import "github.com/pin/tftp/v3"
import "log"
import "time"
import "flag"

func main() {
	var strFile string
	var timeOutInt int
	flag.StringVar(&strFile, "f", "pxelinux.0","File to Download")
	flag.IntVar(&timeOutInt, "t", 10, "Timeout")
	address := flag.String("a", "127.0.0.1", "Server Address")
	port := flag.String("p", "69", "Server Port")
	flag.Parse()

	fullAddr := fmt.Sprintf("%s:%s", *address, *port)

	log.Printf("=================================")
	log.Printf("Trying to esablish connection with %s", fullAddr)
	log.Printf("Timeout is %d", timeOutInt)
	log.Printf("Request for file: %s", strFile)
	log.Printf("=================================")
	log.Printf("...")
	c, err := tftp.NewClient(fullAddr)
	if err != nil {
		fmt.Printf("Failed to established connection %v", err)
	}

	c.SetTimeout(time.Duration(timeOutInt) * time.Second)

	wt, err := c.Receive(strFile, "octet")
	if err != nil {
		log.Fatalf("Failed to request file %v", err)
	}

	wFile, err := os.Create("downloaded_dump.bin")
	if err != nil {
		fmt.Printf("Failed to create local file %v", err)
	}

	defer wFile.Close()
	
	n, err := wt.WriteTo(wFile)
	if err != nil {
		fmt.Printf("Failed to download %v", err)
	}
	
	log.Printf("Success transferred %d Byte", n)

}
