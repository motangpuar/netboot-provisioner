package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/motangpuar/o2-ims-worker/internal/config"
	"github.com/motangpuar/o2-ims-worker/internal/db"
	"github.com/motangpuar/o2-ims-worker/internal/dhcp"
	"github.com/motangpuar/o2-ims-worker/internal/http"
	"github.com/motangpuar/o2-ims-worker/internal/tftp"
)

//import "github.com/motangpuar/o2-ims-worker/internal/ansible"

//"time"

func main()  {

	// --------<*>----------
	disableTFTP := flag.Bool("no-tftp",false,"Disable TFTP")
	disableDHCP := flag.Bool("no-dhcp",false,"Disable DHCP")
	disableHTTP := flag.Bool("no-http",false,"Disable HTTP")
	flag.Parse()

	// tConfig, dConfig := config.Gather()
	cfg := config.Gather()

	// FileData Watcher
	log.Println("[*]------------------------------------------------------")
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	defer watcher.Close()

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Name == "inputs/clients.csv" {
					log.Println("[*] Filename Filter:", event.Name) 
					filedata.Populate(cfg.General.GetSecret())
					continue
				}
				log.Printf("File Watcher Event: %s ", event.String()) 
				log.Printf("Doing nothing on: %s ", event.Name) 
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("File Watcher Error:", err)
			}
		}
	}()

	err = watcher.Add("./inputs/")
	if err != nil {
		log.Fatal(err)
	}

	// Init filedata 
	filedata.Populate(cfg.General.GetSecret())

	// Create Pointer for TFTP & DHCP Config
	tftpCfgPtr := cfg.TFTP
	dhcpCfgPtr := cfg.DHCP

	//----------------------------------------------
	log.Println("[DHCP].........")
	log.Println(dhcpCfgPtr.BindAddr())
	log.Println(dhcpCfgPtr.Enabled())
	log.Println(dhcpCfgPtr.Mode())
	log.Println(dhcpCfgPtr.BindInterface())
	log.Println(dhcpCfgPtr.TFTPIP())
	log.Println(dhcpCfgPtr.TFTPPort())
	log.Println(dhcpCfgPtr.NextServe())
	log.Println(dhcpCfgPtr.BootFilePath())
	log.Println()

	//----------------------------------------------
	log.Println("[TFTP].........")
	log.Println(tftpCfgPtr.BindAddr())
	log.Println(tftpCfgPtr.Enabled())
	log.Println(tftpCfgPtr.BindAddr())
	log.Println(tftpCfgPtr.BindPort())
	log.Println(tftpCfgPtr.BlockSize())
	log.Println()


	ctx,cancel := context.WithCancel(context.Background())
	defer cancel()

	if *disableTFTP != true {
		e := tftp.NewEngine(tftpCfgPtr)
		go e.Start()
	}

	if *disableDHCP != true {
		d := dhcp.NewEngine(dhcpCfgPtr)
		go d.Start()
	}

	if *disableHTTP != true {
		go http_handler.Serve(ctx)
	}

	// Wait for it to stop
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	cancel()

}


